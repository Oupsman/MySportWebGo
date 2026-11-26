package models

import (
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Cheer is a user cheering an activity they may see, once per activity. NotifiedAt is when the owner of the
// activity was told, nil until then (ClaimCheers).
type Cheer struct {
	ActivityID uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID     uint      `gorm:"primaryKey"`
	CreatedAt  time.Time
	NotifiedAt *time.Time
}

// Comment is a comment of a user on an activity they may see
type Comment struct {
	ID         uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	ActivityID uuid.UUID `gorm:"type:uuid"`
	UserID     uint
	Body       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ActivityAccess is what deciding who may see and react to an activity reads of it: not its records
type ActivityAccess struct {
	ID         uuid.UUID
	OwnerID    uint
	Visibility int
	Title      string
	// whether its owner opened it to comments (Activity.CanComments)
	CanComments bool
	// whether its owner wants to be notified of the cheers and comments (Users.NotifySocial)
	OwnerNotifySocial bool
}

// GetActivityAccess returns the owner, the visibility and the title of an activity, gorm.ErrRecordNotFound
// for an unknown one
func (db *DB) GetActivityAccess(id uuid.UUID) (ActivityAccess, error) {
	var access ActivityAccess
	result := db.Table("activities a").
		Select("a.id, a.user_id AS owner_id, a.visibility, a.title, COALESCE(a.can_comments, false) AS can_comments, "+
			"u.notify_social AS owner_notify_social").
		Joins("JOIN users u ON u.id = a.user_id").Where("a.id = ?", id).Limit(1).Scan(&access)
	if result.Error == nil && result.RowsAffected == 0 {
		return access, gorm.ErrRecordNotFound
	}
	return access, result.Error
}

// SocialUser is how a user shows to the others: never their id nor their email
type SocialUser struct {
	UUID     uuid.UUID `json:"uuid"`
	Username string    `json:"username"`
}

// CheerView is a cheer of an activity, with who gave it
type CheerView struct {
	UserID    uint      `json:"-"`
	UUID      uuid.UUID `json:"uuid"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// AddCheer records the cheer of a user on an activity; cheering again changes nothing
func (db *DB) AddCheer(activityID uuid.UUID, userID uint) error {
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&Cheer{ActivityID: activityID, UserID: userID}).Error
}

// RemoveCheer removes the cheer of a user on an activity, if any
func (db *DB) RemoveCheer(activityID uuid.UUID, userID uint) error {
	return db.Where("activity_id = ? AND user_id = ?", activityID, userID).Delete(&Cheer{}).Error
}

// GetCheers returns the cheers of an activity, oldest first
func (db *DB) GetCheers(activityID uuid.UUID) ([]CheerView, error) {
	cheers := []CheerView{}
	err := db.Table("cheers c").Select("c.user_id, u.uuid, u.username, c.created_at").
		Joins("JOIN users u ON u.id = c.user_id").Where("c.activity_id = ?", activityID).
		Order("c.created_at, u.username").Scan(&cheers).Error
	return cheers, err
}

// CommentView is a comment of an activity, with its author
type CommentView struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uint       `json:"-"`
	Author    SocialUser `json:"author" gorm:"embedded;embeddedPrefix:author_"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// GetComments returns the comments of an activity, oldest first
func (db *DB) GetComments(activityID uuid.UUID) ([]CommentView, error) {
	comments := []CommentView{}
	err := db.Table("comments c").
		Select("c.id, c.user_id, u.uuid AS author_uuid, u.username AS author_username, c.body, c.created_at, c.updated_at").
		Joins("JOIN users u ON u.id = c.user_id").Where("c.activity_id = ?", activityID).
		Order("c.created_at, c.id").Scan(&comments).Error
	return comments, err
}

// CountComments returns the number of comments of an activity
func (db *DB) CountComments(activityID uuid.UUID) (int64, error) {
	var n int64
	err := db.Model(&Comment{}).Where("activity_id = ?", activityID).Count(&n).Error
	return n, err
}

// CreateComment saves a new comment
func (db *DB) CreateComment(comment *Comment) error {
	return db.Create(comment).Error
}

// GetComment returns a comment of an activity, gorm.ErrRecordNotFound when the activity has no such comment
func (db *DB) GetComment(activityID, id uuid.UUID) (Comment, error) {
	var comment Comment
	err := db.Where("id = ? AND activity_id = ?", id, activityID).First(&comment).Error
	return comment, err
}

// UpdateComment changes the text of a comment
func (db *DB) UpdateComment(comment *Comment, body string) error {
	return db.Model(comment).Updates(map[string]interface{}{"body": body, "updated_at": time.Now()}).Error
}

// DeleteComment deletes a comment
func (db *DB) DeleteComment(id uuid.UUID) error {
	return db.Where("id = ?", id).Delete(&Comment{}).Error
}

// SetNotifySocial turns on or off the notifications of the cheers and comments on the activities of a user
func (db *DB) SetNotifySocial(userID uint, on bool) error {
	return db.Model(&Users{}).Where("id = ?", userID).Update("notify_social", on).Error
}

// CheerBatch is the cheers of an activity its owner wasn't told of yet, by the usernames of who gave them
// (oldest first)
type CheerBatch struct {
	ActivityID uuid.UUID
	Title      string
	OwnerID    uint
	Usernames  []string
}

// ClaimCheers marks as notified the cheers not notified yet given before before, and calls notify, in the
// same transaction, with those of each activity whose owner wants to be notified: a cheer is notified once,
// and a failed notify leaves them to the next call. The cheers of the owners who don't want to be notified
// are marked without notify.
func (db *DB) ClaimCheers(now, before time.Time, notify func(tx *DB, batch CheerBatch) error) (int, error) {
	notified := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ActivityID   uuid.UUID
			Title        string
			OwnerID      uint
			NotifySocial bool
			Username     string
			CreatedAt    time.Time
		}
		err := tx.Raw(`UPDATE cheers c SET notified_at = ?
			FROM activities a JOIN users o ON o.id = a.user_id, users u
			WHERE c.notified_at IS NULL AND c.created_at < ? AND a.id = c.activity_id AND u.id = c.user_id
			RETURNING c.activity_id, a.title, a.user_id AS owner_id, o.notify_social, u.username, c.created_at`,
			now, before).Scan(&rows).Error
		if err != nil {
			return err
		}
		// RETURNING has no order: the oldest cheer first, then by activity in the order of their first cheer
		sort.SliceStable(rows, func(i, j int) bool {
			if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
				return rows[i].CreatedAt.Before(rows[j].CreatedAt)
			}
			return rows[i].Username < rows[j].Username
		})
		var order []uuid.UUID
		batches := map[uuid.UUID]*CheerBatch{}
		for _, r := range rows {
			if !r.NotifySocial {
				continue
			}
			b, ok := batches[r.ActivityID]
			if !ok {
				b = &CheerBatch{ActivityID: r.ActivityID, Title: r.Title, OwnerID: r.OwnerID}
				batches[r.ActivityID] = b
				order = append(order, r.ActivityID)
			}
			b.Usernames = append(b.Usernames, r.Username)
		}
		txDB := &DB{DB: *tx}
		for _, id := range order {
			if err := notify(txDB, *batches[id]); err != nil {
				return err
			}
			notified++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return notified, nil
}

// SocialExportItem is a cheer (without Body) or a comment in an export
type SocialExportItem struct {
	ActivityID uuid.UUID  `json:"activity_id"`
	Username   string     `json:"username,omitempty"` // who gave it, on the activities of the user
	Body       string     `json:"body,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
}

// SocialExport is what a user gave and received of the cheers and comments
type SocialExport struct {
	CheersGiven      []SocialExportItem `json:"cheers_given"`
	CommentsGiven    []SocialExportItem `json:"comments_given"`
	CheersReceived   []SocialExportItem `json:"cheers_received"`
	CommentsReceived []SocialExportItem `json:"comments_received"`
}

// GetSocialExport returns the cheers and comments a user gave, and those of the others on their activities
func (db *DB) GetSocialExport(userID uint) (SocialExport, error) {
	export := SocialExport{CheersGiven: []SocialExportItem{}, CommentsGiven: []SocialExportItem{},
		CheersReceived: []SocialExportItem{}, CommentsReceived: []SocialExportItem{}}
	queries := []struct {
		into  *[]SocialExportItem
		query *gorm.DB
	}{
		{&export.CheersGiven, db.Table("cheers x").Select("x.activity_id, x.created_at").Where("x.user_id = ?", userID)},
		{&export.CommentsGiven, db.Table("comments x").Select("x.activity_id, x.body, x.created_at, x.updated_at").
			Where("x.user_id = ?", userID)},
		{&export.CheersReceived, db.Table("cheers x").Select("x.activity_id, u.username, x.created_at").
			Joins("JOIN activities a ON a.id = x.activity_id").Joins("JOIN users u ON u.id = x.user_id").
			Where("a.user_id = ? AND x.user_id <> ?", userID, userID)},
		{&export.CommentsReceived, db.Table("comments x").
			Select("x.activity_id, u.username, x.body, x.created_at, x.updated_at").
			Joins("JOIN activities a ON a.id = x.activity_id").Joins("JOIN users u ON u.id = x.user_id").
			Where("a.user_id = ? AND x.user_id <> ?", userID, userID)},
	}
	for _, q := range queries {
		if err := q.query.Order("x.created_at").Scan(q.into).Error; err != nil {
			return export, err
		}
	}
	return export, nil
}
