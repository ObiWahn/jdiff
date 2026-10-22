package diff

// pair is a matched pair of indices into the two compared sequences.
type pair struct{ a, b int }

// maxEditDistance bounds the Myers search. Beyond it the inputs are treated
// as unrelated, which keeps memory at O(D²) for pathological inputs.
const maxEditDistance = 2000

// patience matches a against b using the patience algorithm: lines that are
// unique in both inputs become fixed points, the gaps between them are
// matched recursively, and gaps without unique lines fall back to Myers.
func patience(a, b []string) []pair {
	var out []pair
	patienceRec(a, b, 0, 0, &out)
	return out
}

func patienceRec(a, b []string, ao, bo int, out *[]pair) {
	for len(a) > 0 && len(b) > 0 && a[0] == b[0] {
		*out = append(*out, pair{ao, bo})
		a, b = a[1:], b[1:]
		ao, bo = ao+1, bo+1
	}
	suffix := 0
	for suffix < len(a) && suffix < len(b) && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	a, b = a[:len(a)-suffix], b[:len(b)-suffix]

	if len(a) > 0 && len(b) > 0 {
		fixed := uniqueLIS(a, b)
		if len(fixed) == 0 {
			for _, p := range myers(a, b) {
				*out = append(*out, pair{ao + p.a, bo + p.b})
			}
		} else {
			pa, pb := 0, 0
			for _, p := range fixed {
				patienceRec(a[pa:p.a], b[pb:p.b], ao+pa, bo+pb, out)
				*out = append(*out, pair{ao + p.a, bo + p.b})
				pa, pb = p.a+1, p.b+1
			}
			patienceRec(a[pa:], b[pb:], ao+pa, bo+pb, out)
		}
	}

	for i := range suffix {
		*out = append(*out, pair{ao + len(a) + i, bo + len(b) + i})
	}
}

// uniqueLIS returns the longest sequence of lines that occur exactly once in
// both a and b and appear in the same order in both.
func uniqueLIS(a, b []string) []pair {
	type count struct{ a, b, bi int }
	counts := make(map[string]*count)
	for _, s := range a {
		c := counts[s]
		if c == nil {
			c = &count{}
			counts[s] = c
		}
		c.a++
	}
	for i, s := range b {
		if c := counts[s]; c != nil {
			c.b++
			c.bi = i
		}
	}
	var cand []pair
	for i, s := range a {
		if c := counts[s]; c.a == 1 && c.b == 1 {
			cand = append(cand, pair{i, c.bi})
		}
	}
	if len(cand) == 0 {
		return nil
	}

	// Patience sorting: tails[k] is the index into cand of the smallest b
	// ending an increasing run of length k+1.
	var tails []int
	prev := make([]int, len(cand))
	for i, p := range cand {
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := (lo + hi) / 2
			if cand[tails[mid]].b < p.b {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		prev[i] = -1
		if lo > 0 {
			prev[i] = tails[lo-1]
		}
		if lo == len(tails) {
			tails = append(tails, i)
		} else {
			tails[lo] = i
		}
	}
	lis := make([]pair, len(tails))
	for i, k := len(tails)-1, tails[len(tails)-1]; i >= 0; i, k = i-1, prev[k] {
		lis[i] = cand[k]
	}
	return lis
}

// myers returns a longest common subsequence of a and b as matched index
// pairs, or nil when the edit distance exceeds maxEditDistance.
func myers[T comparable](a, b []T) []pair {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return nil
	}
	limit := min(n+m, maxEditDistance)
	off := limit + 1
	v := make([]int, 2*limit+3)
	// trace[d] holds v[-d..d] as it was before step d.
	var trace [][]int
	for d := 0; d <= limit; d++ {
		trace = append(trace, append([]int(nil), v[off-d:off+d+1]...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[off+k] = x
			if x >= n && y >= m {
				return myersBacktrack(trace, n, m)
			}
		}
	}
	return nil
}

func myersBacktrack(trace [][]int, n, m int) []pair {
	var out []pair
	x, y := n, m
	for d := len(trace) - 1; d > 0; d-- {
		v := trace[d]
		at := func(k int) int { return v[k+d] }
		k := x - y
		var pk int
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := at(pk)
		py := px - pk
		for x > px && y > py {
			x, y = x-1, y-1
			out = append(out, pair{x, y})
		}
		x, y = px, py
	}
	for x > 0 && y > 0 {
		x, y = x-1, y-1
		out = append(out, pair{x, y})
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
