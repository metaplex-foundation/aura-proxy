package models

type TokenType string

const (
	DefaultTokenType         TokenType = "default"           // behaviour as without token
	SpeedTokenType           TokenType = "speed"             // give priority to fast nodes
	ReliableTokenType        TokenType = "reliable"          // give priority to reliable nodes
	OnlyPublicNodesTokenType TokenType = "only_public_nodes" // for test purposes
	UnlimitedTokenType       TokenType = "unlimited"         // rate limit doesn't apply
	BasicTokenType           TokenType = "basic"             // 50 rps
	ProTokenType             TokenType = "pro"               // 100 rps

	defaultReqPerSecondLimit int64 = 5
)

func (t TokenType) UseFirstEndpoint() bool {
	return t == DefaultTokenType || t == UnlimitedTokenType || t == OnlyPublicNodesTokenType || t == BasicTokenType || t == ProTokenType
}
func (t TokenType) IsTokenRateLimited() bool { // currently all tokens (except DefaultTokenType, BasicTokenType, ProTokenType) are used without rate limits
	return t == DefaultTokenType || t == BasicTokenType || t == ProTokenType
}

type TokenInfo struct {
	TokenType TokenType
	Tracked   bool
}

func (t TokenType) GetReqPerSecond() int64 {
	switch t {
	case DefaultTokenType:
		return defaultReqPerSecondLimit
	case SpeedTokenType, ReliableTokenType, OnlyPublicNodesTokenType, UnlimitedTokenType:
		return 0
	case BasicTokenType:
		return 50 //nolint:revive
	case ProTokenType:
		return 100 //nolint:revive
	}

	return defaultReqPerSecondLimit
}
