package nodeset

import (
	"strings"
)

func Merge(nodestr ...string) (string, error) {
	t1, err := NewNodeSet(strings.Join(nodestr, ","))
	if err != nil {
		return "", err
	}
	return t1.String(), nil
}

func Expand(nodestr string) ([]string, error) {
	n1, err := NewNodeSet(nodestr)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0)

	it := n1.Iterator()
	for it.Next() {
		result = append(result, it.Value())
	}
	return result, nil
}

func Yield(nodestr string) (*NodeSetIterator, error) {
	n1, err := NewNodeSet(nodestr)
	if err != nil {
		return nil, err
	}
	return n1.Iterator(), nil
}

func Split(nodelist []string, w int) [][]string {
	splitNodelist := make([][]string, 0)
	if len(nodelist) == 0 {
		return splitNodelist
	}
	nodesNum := len(nodelist)
	if nodesNum <= w {
		for _, node := range nodelist {
			splitNodelist = append(splitNodelist, []string{node})
		}
	} else {
		splitNums := nodesNum / w
		leaveNums := nodesNum % w
		i := 0
		for i = 0; i < w; i++ {
			s := i * splitNums
			e := (i + 1) * splitNums
			if i < leaveNums {
				if i == 0 {
					e++
				} else {
					s += i
					e += i + 1
				}
			} else {
				s += leaveNums
				e += leaveNums
			}
			splitNodelist = append(splitNodelist, nodelist[s:e])
		}
	}
	return splitNodelist
}
