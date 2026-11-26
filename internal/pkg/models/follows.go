package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Follow is a user following another one: AcceptedAt nil is a request the followee hasn't accepted yet
// (Users.FollowApproval)
type Follow struct {
	FollowerID uint `gorm:"primaryKey"`
	FolloweeID uint `gorm:"primaryKey"`
	CreatedAt  time.Time
	AcceptedAt *time.Time
}

// The states of a follow, as the follower sees it
const (
	FollowNone      = "none"
	FollowPending   = "pending"
	FollowFollowing = "following"
)

// followState is the state of a follow, FollowNone for none
func followState(f *Follow) string {
	switch {
	case f == nil:
		return FollowNone
	case f.AcceptedAt == nil:
		return FollowPending
	default:
		return FollowFollowing
	}
}

// GetFollowState returns the state of the follow of followee by follower
func (db *DB) GetFollowState(followerID, followeeID uint) (string, error) {
	var follows []Follow
	err := db.Where("follower_id = ? AND followee_id = ?", followerID, followeeID).Limit(1).Find(&follows).Error
	if err != nil || len(follows) == 0 {
		return FollowNone, err
	}
	return followState(&follows[0]), nil
}

// RequestFollow makes follower follow followee: accepted at once when accept, else a request; created tells
// whether it is new (a follow already there is left as it is). It returns the state of the follow.
func (db *DB) RequestFollow(followerID, followeeID uint, accept bool) (state string, created bool, err error) {
	follow := Follow{FollowerID: followerID, FolloweeID: followeeID}
	if accept {
		now := time.Now()
		follow.AcceptedAt = &now
	}
	result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&follow)
	if result.Error != nil {
		return "", false, result.Error
	}
	state, err = db.GetFollowState(followerID, followeeID)
	return state, result.RowsAffected > 0, err
}

// RemoveFollow removes the follow (or the request) of followee by follower; false when there was none
func (db *DB) RemoveFollow(followerID, followeeID uint) (bool, error) {
	result := db.Where("follower_id = ? AND followee_id = ?", followerID, followeeID).Delete(&Follow{})
	return result.RowsAffected > 0, result.Error
}

// AcceptFollower accepts the request of follower to follow followee, gorm.ErrRecordNotFound without one
func (db *DB) AcceptFollower(followeeID, followerID uint) error {
	result := db.Model(&Follow{}).Where("follower_id = ? AND followee_id = ? AND accepted_at IS NULL", followerID, followeeID).
		Update("accepted_at", time.Now())
	if result.Error == nil && result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

// SetFollowApproval sets whether a user accepts each follower; turning it off accepts the requests waiting
func (db *DB) SetFollowApproval(userID uint, on bool) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Users{}).Where("id = ?", userID).Update("follow_approval", on).Error; err != nil {
			return err
		}
		if on {
			return nil
		}
		return tx.Model(&Follow{}).Where("followee_id = ? AND accepted_at IS NULL", userID).
			Update("accepted_at", time.Now()).Error
	})
}

// FollowView is a follower or a followee of a user
type FollowView struct {
	SocialUser
	CreatedAt  time.Time  `json:"created_at"`
	AcceptedAt *time.Time `json:"accepted_at"` // nil: a request
}

// GetFollows returns the followers of a user (followers) or the users they follow, with the requests, the
// most recent first; the disabled users are left out
func (db *DB) GetFollows(userID uint, followers bool) ([]FollowView, error) {
	mine, theirs := "followee_id", "follower_id"
	if !followers {
		mine, theirs = theirs, mine
	}
	views := []FollowView{}
	err := db.Table("follows f").Select("u.uuid, u.username, f.created_at, f.accepted_at").
		Joins("JOIN users u ON u.id = f."+theirs).
		Where("f."+mine+" = ? AND u.disabled_at IS NULL", userID).
		Order("f.created_at DESC, u.username").Scan(&views).Error
	return views, err
}

// FollowCounts are the accepted followers of a user and the users they follow
type FollowCounts struct {
	Followers int64 `json:"followers"`
	Following int64 `json:"following"`
}

// GetFollowCounts returns the follow counts of a user, the disabled users left out
func (db *DB) GetFollowCounts(userID uint) (FollowCounts, error) {
	var counts FollowCounts
	err := db.Raw(`SELECT
		(SELECT count(*) FROM follows f JOIN users u ON u.id = f.follower_id
			WHERE f.followee_id = ? AND f.accepted_at IS NOT NULL AND u.disabled_at IS NULL) AS followers,
		(SELECT count(*) FROM follows f JOIN users u ON u.id = f.followee_id
			WHERE f.follower_id = ? AND f.accepted_at IS NOT NULL AND u.disabled_at IS NULL) AS following`,
		userID, userID).Scan(&counts).Error
	return counts, err
}

// UserCard is a user found by a search, with the state of their follow by the searcher
type UserCard struct {
	SocialUser
	State string `json:"state"`
}

// likeEscape escapes the wildcards of LIKE in s
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// SearchUsers returns the enabled users but viewer whose username contains q (case ignored), the ones
// starting with it first, at most limit
func (db *DB) SearchUsers(viewerID uint, q string, limit int) ([]UserCard, error) {
	var rows []struct {
		SocialUser
		AcceptedAt *time.Time
		Requested  bool
	}
	pattern := likeEscape(strings.ToLower(q))
	err := db.Table("users u").
		Select("u.uuid, u.username, f.accepted_at, f.follower_id IS NOT NULL AS requested").
		Joins("LEFT JOIN follows f ON f.followee_id = u.id AND f.follower_id = ?", viewerID).
		Where("u.id <> ? AND u.disabled_at IS NULL AND lower(u.username) LIKE ?", viewerID, "%"+pattern+"%").
		Order(clause.OrderBy{Expression: clause.Expr{SQL: "lower(u.username) LIKE ? DESC, lower(u.username)",
			Vars: []interface{}{pattern + "%"}, WithoutParentheses: true}}).
		Limit(limit).Scan(&rows).Error
	cards := make([]UserCard, len(rows))
	for i, r := range rows {
		state := FollowNone
		if r.Requested {
			state = followState(&Follow{AcceptedAt: r.AcceptedAt})
		}
		cards[i] = UserCard{SocialUser: r.SocialUser, State: state}
	}
	return cards, err
}

// FeedItem is an activity in the feed or on the profile of a user: what its card shows, never its trace
type FeedItem struct {
	ID           uuid.UUID  `json:"id"`
	Owner        SocialUser `json:"owner" gorm:"embedded;embeddedPrefix:owner_"`
	Title        string     `json:"title"`
	Date         time.Time  `json:"date"`
	Sport        string     `json:"sport"`
	Distance     float64    `json:"distance"`
	Duration     uint32     `json:"duration"`
	Elevation    float64    `json:"elevation"`
	AvgSpeed     float64    `json:"avg_speed"`
	HasMap       bool       `json:"-"`
	Thumbnail    string     `json:"thumbnail"` // set by the caller, from HasMap
	CheerCount   int64      `json:"cheer_count"`
	CommentCount int64      `json:"comment_count"`
	Cheered      bool       `json:"cheered"` // by the viewer
}

// FeedCursor is where a page of the feed starts: the activities before this date, and before this id at
// that date; zero for the first page
type FeedCursor struct {
	Date time.Time
	ID   uuid.UUID
}

// feedQuery selects the cards of the activities, as viewer sees them, from the cursor, at most limit; where
// chooses the activities: it must keep the ones viewer may see only
func (db *DB) feedQuery(viewerID uint, cursor FeedCursor, limit int, where string, args ...interface{}) ([]FeedItem, error) {
	items := []FeedItem{}
	query := db.Table("activities").
		Select("activities.id, o.uuid AS owner_uuid, o.username AS owner_username, activities.title, "+
			"activities.date, activities.sport, activities.distance, activities.duration, "+
			"activities.positive_elevation AS elevation, activities.avg_speed, "+HasMapColumn+", "+
			"(SELECT count(*) FROM cheers WHERE cheers.activity_id = activities.id) AS cheer_count, "+
			"(SELECT count(*) FROM comments WHERE comments.activity_id = activities.id) AS comment_count, "+
			"EXISTS (SELECT 1 FROM cheers WHERE cheers.activity_id = activities.id AND cheers.user_id = ?) AS cheered",
			viewerID).
		Joins("JOIN users o ON o.id = activities.user_id").
		Where("o.disabled_at IS NULL").Where(where, args...)
	if !cursor.Date.IsZero() {
		query = query.Where("(activities.date, activities.id) < (?, ?)", cursor.Date, cursor.ID)
	}
	err := query.Order("activities.date DESC, activities.id DESC").Limit(limit).Scan(&items).Error
	return items, err
}

// visibleToUsers are the visibilities of the activities the logged in users see (not the private ones)
var visibleToUsers = []int{1, 2}

// GetFeed returns the activities of the users viewer follows (accepted) that they may see, the most recent
// first
func (db *DB) GetFeed(viewerID uint, cursor FeedCursor, limit int) ([]FeedItem, error) {
	return db.feedQuery(viewerID, cursor, limit,
		"activities.user_id IN (SELECT followee_id FROM follows WHERE follower_id = ? AND accepted_at IS NOT NULL) "+
			"AND activities.visibility IN ?", viewerID, visibleToUsers)
}

// GetProfileActivities returns the activities of owner viewer may see, the most recent first: all theirs
// for the owner, the ones not private for the others
func (db *DB) GetProfileActivities(ownerID, viewerID uint, cursor FeedCursor, limit int) ([]FeedItem, error) {
	if ownerID == viewerID {
		return db.feedQuery(viewerID, cursor, limit, "activities.user_id = ?", ownerID)
	}
	return db.feedQuery(viewerID, cursor, limit, "activities.user_id = ? AND activities.visibility IN ?",
		ownerID, visibleToUsers)
}
