package packer

type Tokenizer interface {
	CountTokens(text string) int
}

type HeuristicTokenizer struct {
	charsPerToken float64
}

// NewHeuristicTokenizer creates an estimator using a simple character-to-token ratio
func NewHeuristicTokenizer(charsPerToken float64) *HeuristicTokenizer {
	if charsPerToken <= 0 {
		charsPerToken = 4.2 // 1 token is roughly 4.2 characters on average in Markdown/code text
	}
	return &HeuristicTokenizer{charsPerToken: charsPerToken}
}

// CountTokens estimates the number of tokens in the text
func (ht *HeuristicTokenizer) CountTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return int(float64(len(text)) / ht.charsPerToken)
}
