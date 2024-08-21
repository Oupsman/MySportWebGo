package types

type NotificationsBody struct {
	Message         string `json:"message"`
	Config          string `json:"config"`
	ChannelProvider string `json:"channel_provider"`
	ID              uint   `json:"id"`
}

var Genders = []string{"Male", "Female"}

var Units = []string{"Imperial", "Metric"}

type FloatArray []float64

type GpsPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type ChannelBody struct {
	ChannelID       uint64 `json:"channel_id"`
	ChannelProvider string `json:"channel_provider"`
	Config          string `json:"config"`
	Name            string `json:"name"`
	UserID          uint64 `json:"user_id"`
}

type UserBody struct {
	ID               uint64  `json:"user_id"`
	Username         string  `json:"username"`
	Password         string  `json:"password"`
	OldPassword      string  `json:"oldpassword"`
	Email            string  `json:"email"`
	Weight           uint16  `json:"weight"`
	Unit             int     `json:"unit"`
	Timezone         string  `json:"timezone"`
	Gender           int     `json:"gender"`
	Height           float32 `json:"height"`
	MaxHR            int     `json:"max_hr"`
	WeightObjective  int     `json:"weight_objective"`
	SecurityDistance int     `json:"security_distance"`
}

type Device struct {
	Model  string `json:"model"`
	Brand  string `json:"brand"`
	Serial string `json:"serial_number"`
}
