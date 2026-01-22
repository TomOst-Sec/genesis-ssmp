package testkit

// TokenCount estimates token count and reports raw byte size.
// Uses the same formula as god.EstimateTokens: ceil(bytes/4) + 10 overhead.
func TokenCount(data []byte) (tokens int, bytes int) {
	bytes = len(data)
	tokens = bytes/4 + 10
	return tokens, bytes
}
