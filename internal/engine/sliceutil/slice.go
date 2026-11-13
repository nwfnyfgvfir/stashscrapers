package sliceutil

func Unique(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
func Delete(in []string, value string) []string {
	out := []string{}
	for _, s := range in {
		if s != value {
			out = append(out, s)
		}
	}
	return out
}
