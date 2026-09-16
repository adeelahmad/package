// diff.go — unified diff of one file between two versions. Plain LCS over lines;
// handoff documents are small text files, so no fancier algorithm is warranted.
package main

import (
	"bytes"
	"fmt"
	"strings"
)

const (
	diffContext = 3
	diffMaxArea = 4_000_000 // lines(a) × lines(b) cap before we refuse a full diff
)

type diffOp struct {
	kind byte // ' ' equal, '-' only in a, '+' only in b
	line string
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

func isBinary(b []byte) bool { return bytes.IndexByte(b, 0) >= 0 }

func lcsOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	// dp[i][j] = LCS length of a[i:], b[j:]
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

// unifiedDiff returns "" when the contents are identical.
func unifiedDiff(fromName, toName string, a, b []byte) string {
	if bytes.Equal(a, b) {
		return ""
	}
	if isBinary(a) || isBinary(b) {
		return fmt.Sprintf("Binary files %s and %s differ\n", fromName, toName)
	}
	al, bl := splitLines(a), splitLines(b)
	if len(al)*len(bl) > diffMaxArea {
		return fmt.Sprintf("Files %s and %s differ (too large for a line diff: %d × %d lines)\n", fromName, toName, len(al), len(bl))
	}
	ops := lcsOps(al, bl)

	// prefix counts of a-lines and b-lines consumed before op k
	aBefore := make([]int, len(ops)+1)
	bBefore := make([]int, len(ops)+1)
	for k, op := range ops {
		aBefore[k+1], bBefore[k+1] = aBefore[k], bBefore[k]
		if op.kind != '+' {
			aBefore[k+1]++
		}
		if op.kind != '-' {
			bBefore[k+1]++
		}
	}

	// hunks: changed runs expanded by context, merged when they touch
	type span struct{ s, e int }
	var hunks []span
	for k := 0; k < len(ops); {
		if ops[k].kind == ' ' {
			k++
			continue
		}
		e := k
		for e < len(ops) && ops[e].kind != ' ' {
			e++
		}
		h := span{max(0, k-diffContext), min(len(ops), e+diffContext)}
		if len(hunks) > 0 && h.s <= hunks[len(hunks)-1].e {
			hunks[len(hunks)-1].e = h.e
		} else {
			hunks = append(hunks, h)
		}
		k = e
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", fromName, toName)
	for _, h := range hunks {
		aStart, bStart := aBefore[h.s]+1, bBefore[h.s]+1
		aLen, bLen := aBefore[h.e]-aBefore[h.s], bBefore[h.e]-bBefore[h.s]
		if aLen == 0 {
			aStart--
		}
		if bLen == 0 {
			bStart--
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", aStart, aLen, bStart, bLen)
		for _, op := range ops[h.s:h.e] {
			sb.WriteByte(op.kind)
			sb.WriteString(op.line)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
