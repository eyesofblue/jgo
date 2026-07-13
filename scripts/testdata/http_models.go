package model

type UserInfo struct {
	UID      int64             `json:"uid"`
	Name     string            `json:"name"`
	Profile  UserProfile       `json:"profile"`
	Tags     []string          `json:"tags"`
	Metadata map[string]string `json:"metadata"`
}

type UserProfile struct {
	Avatar string `json:"avatar"`
	Bio    string `json:"bio"`
}

type UpdateUserRequest struct {
	UID     int64       `json:"uid"`
	Name    string      `json:"name"`
	Profile UserProfile `json:"profile"`
	Tags    []string    `json:"tags"`
}
