package types

type NotificationsBody struct {
	Message         string `json:"message"`
	Config          string `json:"config"`
	ChannelProvider string `json:"channel_provider"`
	ID              uint   `json:"id"`
}

var Genders = []string{"Male", "Female"}

var Units = []string{"Imperial", "Metric"}
