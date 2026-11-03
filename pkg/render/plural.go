package render

// Plural returns singular when n is one and plural otherwise. Summary labels use
// it so a count of one never reads "1 repositories".
func Plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
