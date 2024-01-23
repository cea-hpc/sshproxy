package nodeset

import (
	"fmt"
	"sort"

	"github.com/segmentio/fasthash/fnv1a"
)

type NodeSetIterator struct {
	nodes   []string
	current int
}

type RangeSetItem struct {
	value   int
	padding int
}

type RangeSetNDIterator struct {
	vects   [][]*RangeSetItem
	seen    map[uint64]struct{}
	current int
}

func (i *NodeSetIterator) Next() bool {
	i.current++

	return i.current < len(i.nodes)
}

func (i *NodeSetIterator) Len() int {
	return len(i.nodes)
}

func (i *NodeSetIterator) Value() string {
	return i.nodes[i.current]
}

func NewRangeSetNDIterator() *RangeSetNDIterator {
	return &RangeSetNDIterator{
		vects:   make([][]*RangeSetItem, 0),
		seen:    make(map[uint64]struct{}),
		current: -1,
	}
}

func (i *RangeSetNDIterator) Next() bool {
	i.current++

	return i.current < len(i.vects)
}

func (i *RangeSetNDIterator) Len() int {
	return len(i.vects)
}

func (i *RangeSetNDIterator) IntValue() []int {
	vals := make([]int, 0, len(i.vects[i.current]))
	for _, v := range i.vects[i.current] {
		vals = append(vals, v.value)
	}
	return vals
}

func (i *RangeSetNDIterator) FormatList() []interface{} {
	vals := make([]interface{}, 0, len(i.vects[i.current]))
	for _, v := range i.vects[i.current] {
		vals = append(vals, fmt.Sprintf("%0*d", v.padding, v.value))
	}
	return vals
}

func (it *RangeSetNDIterator) Sort() {
	sort.SliceStable(it.vects, func(i, j int) bool {
		for x := range it.vects[i] {
			if it.vects[i][x].value != it.vects[j][x].value {
				return it.vects[i][x].value < it.vects[j][x].value
			}
		}
		return false
	})
}

func (it *RangeSetNDIterator) product(result []*RangeSetItem, params ...[]*RangeSetItem) {
	if len(params) == 0 {
		hash := fnv1a.Init64
		for _, i := range result {
			hash = fnv1a.AddUint64(hash, uint64(i.value))
		}

		if _, ok := it.seen[hash]; !ok {
			it.seen[hash] = struct{}{}
			it.vects = append(it.vects, result)
		}

		return
	}

	p, params := params[0], params[1:]
	for i := 0; i < len(p); i++ {
		resultCopy := append([]*RangeSetItem{}, result...)
		it.product(append(resultCopy, p[i]), params...)
	}
}
