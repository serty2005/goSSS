package dtos

import "time"

type AgentRegistrationResponseDTO struct {
	Status                string    `json:"status"`
	Message               string    `json:"message,omitempty"`
	AgentUUID             string    `json:"agent_uuid"`
	AccessToken           string    `json:"access_token,omitempty"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at,omitzero"`
	RefreshToken          string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at,omitzero"`
}

type AgentTokenRefreshRequestDTO struct {
	AgentUUID    string `json:"agent_uuid"`
	RefreshToken string `json:"refresh_token"`
}

type AgentTokenRefreshResponseDTO struct {
	Status                string    `json:"status"`
	AgentUUID             string    `json:"agent_uuid"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}
