package protect

func FilePath(path string) string {
	i, j, n := 0, -1, len(path)
	in, out := []byte(path), make([]byte, 0, n)

	for i < n {
		switch true {
		case (i == 0 || i+1 >= n) && in[i] == '.':
			i++
		case in[i] == '.' && i+1 < n && (in[i+1] == '/' || in[i+1] == '.'):
			i++
		case in[i] == '/' && i+1 < n && in[i+1] == '/':
			i++
		case in[i] == '/' && i+1 < n && in[i+1] == '.':
			if j >= 0 && out[j] != '/' {
				out = append(out, in[i])
				j++
			}
			i += 2
		default:
			out = append(out, in[i])
			j++
			i++
		}
	}

	return string(out)
}
