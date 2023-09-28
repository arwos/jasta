package utils

import "sort"

func Map2Slice(v map[string]struct{}) []string {
	data := make([]string, 0, len(v))
	for s := range v {
		data = append(data, s)
	}
	sort.Strings(data)
	return data
}
