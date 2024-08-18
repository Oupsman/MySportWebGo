// Package Models
// File: channels.go
// Provides the model for the Channels table in the database and methods to create, delete, update, and get channels.

package models

import (
	"MySportWeb/internal/pkg/types"
	"gorm.io/gorm"
)

type Channel struct {
	gorm.Model
	User            Users
	UserID          uint64 `json:"user_id"`
	ChannelProvider string `json:"channel_provider"`
	Config          string `json:"config"`
	Name            string `json:"name"`
}

func (db *DB) AddChannel(UserID uint64, ChannelProvider string, Config string, Name string) (uint, error) {
	currentUser, err := db.GetUser(UserID)
	if err != nil {
		return 0, err
	}
	var channel = Channel{
		User:            currentUser,
		UserID:          UserID,
		ChannelProvider: ChannelProvider,
		Config:          Config,
		Name:            Name,
	}
	result := db.Create(&channel)
	if result.Error != nil {
		return 0, result.Error
	}
	return channel.ID, nil
}

func (db *DB) DeleteChannel(channelID uint64) error {
	id := int(channelID)

	var channel = Channel{}

	result := db.Find(&channel, id)
	if result.Error != nil {
		return result.Error
	}
	// Permanent delete
	result = db.Unscoped().Delete(&channel)
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (db *DB) UpdateChannel(channelBody types.ChannelBody) error {
	id := int(channelBody.ChannelID)

	var channel = Channel{}
	result := db.Find(&channel, id)
	if result.Error != nil {
		return result.Error
	}
	channel.Name = channelBody.Name
	channel.Config = channelBody.Config
	channel.ChannelProvider = channelBody.ChannelProvider
	channel.UserID = channelBody.UserID

	result = db.Save(&channel)
	if result.Error != nil {
		return result.Error
	}

	return nil
}

func (db *DB) GetChannel(channelID uint64) (types.ChannelBody, error) {
	var channel = Channel{}
	result := db.Find(&channel, channelID)
	if result.Error != nil {
		return types.ChannelBody{}, result.Error
	}
	return types.ChannelBody{
		ChannelID:       channelID,
		ChannelProvider: channel.ChannelProvider,
		Config:          channel.Config,
		Name:            channel.Name,
		UserID:          channel.UserID,
	}, nil
}

func (db *DB) GetChannels(UserID uint64) ([]types.ChannelBody, error) {
	var channels []Channel
	result := db.Find(&channels, "user_id = ?", UserID)
	if result.Error != nil {
		return nil, result.Error
	}
	var channelBodies []types.ChannelBody
	for _, channel := range channels {
		channelBodies = append(channelBodies, types.ChannelBody{
			ChannelID:       uint64(channel.ID),
			ChannelProvider: channel.ChannelProvider,
			Config:          channel.Config,
			Name:            channel.Name,
			UserID:          channel.UserID,
		})
	}
	return channelBodies, nil
}
