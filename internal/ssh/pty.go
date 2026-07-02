package ssh

func NormalizePTYSize(rows, cols int) (int, int) {
	if cols < 40 {
		cols = 80
	}
	if rows < 5 {
		rows = 24
	}
	return rows, cols
}
