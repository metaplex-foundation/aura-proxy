package models

type (
	// SupportedMethod model info
	//	@Description	response_time in ms
	SupportedMethod struct {
		Name           string `json:"name" example:"getSignatureStatuses"`
		ResponseTimeMs int64  `json:"response_time" example:"1000"`
	}
	URLWithMethods struct {
		URL              string
		SupportedMethods []SupportedMethod
		SlotAmount       int64
		IsFullHistory    bool
	}
)
