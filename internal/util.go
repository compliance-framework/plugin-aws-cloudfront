package internal

func StringAddressed(str string) *string {
	return &str
}

func MergeMaps(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, values := range maps {
		for k, v := range values {
			result[k] = v
		}
	}
	return result
}
