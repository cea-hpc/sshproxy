package nodeset

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/willf/bitset"
)

var (
	ErrInvalidRangeSet      = errors.New("invalid range set")
	ErrMismatchedDimensions = errors.New("mismatched dimensions")
	ErrParseRangeSet        = errors.New("rangeset parse error")
	ErrInvalidPadding       = errors.New("invalid padding")
)

type RangeSet struct {
	bits    bitset.BitSet
	padding int
}

type RangeSetND struct {
	ranges [][]*RangeSet
	dirty  bool
}

type Slice struct {
	start int
	stop  int
	step  int
	pad   int
}

func NewRangeSet(pattern string) (rs *RangeSet, err error) {
	rs = &RangeSet{}
	if len(pattern) == 0 {
		// empty range set
		return rs, nil
	}

	for _, subrange := range strings.Split(pattern, ",") {
		err := rs.AddString(subrange)
		if err != nil {
			return nil, err
		}
	}
	// if pattern == "16" {
	// 	rs.padding = 2
	// }
	return rs, nil
}

func (rs *RangeSet) AddString(subrange string) (err error) {
	if subrange == "" {
		return fmt.Errorf("empty range - %w", ErrParseRangeSet)
	}

	baserange := subrange
	step := 1
	if strings.Contains(subrange, "/") {
		parts := strings.SplitN(subrange, "/", 2)
		baserange = parts[0]
		if len(parts) != 2 || parts[1] == "" {
			return fmt.Errorf("cannont parse step %s - %w", subrange, ErrParseRangeSet)
		}

		step, err = strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("cannont convert step to integer %s - %w", subrange, ErrParseRangeSet)
		}
	}

	var start, stop, pad int
	parts := []string{baserange}

	if !strings.Contains(baserange, "-") {
		if step != 1 {
			return fmt.Errorf("invalid step usage %s - %w", subrange, ErrParseRangeSet)
		}
	} else {
		parts = strings.SplitN(baserange, "-", 2)
		if len(parts) != 2 || parts[1] == "" {
			return fmt.Errorf("cannpt parse end value %s - %w", subrange, ErrParseRangeSet)
		}
	}

	start, err = strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("cannont convert starting range to integer %s - %w", parts[0], ErrParseRangeSet)
	}

	// if start != 0 {
	// 	begins := strings.TrimLeft(parts[0], "0")
	// 	if len(parts[0])-len(begins) > 0 {
	// 		pad = len(parts[0])
	// 	}
	// } else {
	// 	if len(parts[0]) > 1 {
	// 		pad = len(parts[0])
	// 	}
	// }
	// pad is origin part length event has a 0 prefix
	pad = len(parts[0])

	if len(parts) == 2 {
		stop, err = strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("cannont convert ending range to integer %s - %w", parts[1], ErrParseRangeSet)
		}
	} else {
		stop = start
	}

	if start > stop || step < 1 {
		return fmt.Errorf("invalid value in range %s - %w", subrange, ErrParseRangeSet)
	}

	return rs.AddSlice(&Slice{start, stop + 1, step, pad})
}

func (rs *RangeSet) AddSlice(slice *Slice) error {
	if slice.start > slice.stop {
		return fmt.Errorf("invalid range start > stop - %w", ErrInvalidRangeSet)
	}
	if slice.step <= 0 {
		return fmt.Errorf("invalid range step <= 0 - %w", ErrInvalidRangeSet)
	}
	if slice.pad < 0 {
		return fmt.Errorf("invalid range padding < 0 - %w", ErrInvalidRangeSet)
	}

	if slice.pad > 0 && rs.padding == 0 {
		rs.padding = slice.pad
	}
	rs.update(slice)

	return nil
}

func (rs *RangeSet) Clone() *RangeSet {
	return &RangeSet{padding: rs.padding, bits: *rs.bits.Clone()}
}

func (rs *RangeSet) Intersection(other *RangeSet) *RangeSet {
	intersec := rs.bits.Intersection(&other.bits)
	return &RangeSet{padding: rs.padding, bits: *intersec}
}

func (rs *RangeSet) InPlaceIntersection(other *RangeSet) {
	rs.bits.InPlaceIntersection(&other.bits)
}

func (rs *RangeSet) Union(other *RangeSet) *RangeSet {
	union := rs.bits.Union(&other.bits)
	return &RangeSet{padding: rs.padding, bits: *union}
}

func (rs *RangeSet) InPlaceUnion(other *RangeSet) {
	rs.bits.InPlaceUnion(&other.bits)
}

func (rs *RangeSet) Difference(other *RangeSet) *RangeSet {
	diff := rs.bits.Difference(&other.bits)
	return &RangeSet{padding: rs.padding, bits: *diff}
}

func (rs *RangeSet) InPlaceDifference(other *RangeSet) {
	rs.bits.InPlaceDifference(&other.bits)
}

func (rs *RangeSet) SymmetricDifference(other *RangeSet) *RangeSet {
	diff := rs.bits.SymmetricDifference(&other.bits)
	return &RangeSet{padding: rs.padding, bits: *diff}
}

func (rs *RangeSet) InPlaceSymmetricDifference(other *RangeSet) {
	rs.bits.InPlaceSymmetricDifference(&other.bits)
}

func (rs *RangeSet) Superset(other *RangeSet) bool {
	return rs.bits.IsSuperSet(&other.bits)
}

func (rs *RangeSet) Subset(other *RangeSet) bool {
	return other.bits.IsSuperSet(&rs.bits)
}

func (rs *RangeSet) Greater(other *RangeSet) bool {
	return rs.Len() > other.Len() && rs.Superset(other)
}

func (rs *RangeSet) Less(other *RangeSet) bool {
	return rs.Len() < other.Len() && rs.Subset(other)
}

func (rs *RangeSet) Equal(other *RangeSet) bool {
	if rs.bits.Count() != other.bits.Count() {
		return false
	}

	for i, e := rs.bits.NextSet(0); e; i, e = rs.bits.NextSet(i + 1) {
		if !other.bits.Test(i) {
			return false
		}
	}

	return true
}

func (rs *RangeSet) Empty() bool {
	return rs.bits.Count() == 0
}

func (rs *RangeSet) Len() int {
	return int(rs.bits.Count())
}

func (rs *RangeSet) String() string {
	var buffer bytes.Buffer
	slices := rs.Slices()
	for i, sli := range slices {
		if sli.start+1 == sli.stop {
			// rs.padding bug
			buffer.WriteString(fmt.Sprintf("%0*d", rs.padding, sli.start))
		} else {
			buffer.WriteString(fmt.Sprintf("%0*d-%0*d", rs.padding, sli.start, rs.padding, sli.stop-1))
		}
		if i != len(slices)-1 {
			buffer.WriteString(",")
		}
	}
	return buffer.String()
}

func (rs *RangeSet) Strings() []string {
	strings := make([]string, 0)
	for _, sli := range rs.Slices() {
		for i := sli.start; i < sli.stop; i += sli.step {
			strings = append(strings, fmt.Sprintf("%0*d", rs.padding, i))
		}
	}

	return strings
}

func (rs *RangeSet) Ints() []int {
	ints := make([]int, 0)
	for _, sli := range rs.Slices() {
		for i := sli.start; i < sli.stop; i += sli.step {
			ints = append(ints, i)
		}
	}

	return ints
}

func (rs *RangeSet) Items() []*RangeSetItem {
	items := make([]*RangeSetItem, 0)
	for _, sli := range rs.Slices() {
		for i := sli.start; i < sli.stop; i += sli.step {
			items = append(items, &RangeSetItem{value: i, padding: rs.padding})
		}
	}

	return items
}

func (rs *RangeSet) update(slice *Slice) {
	for i := slice.start; i < slice.stop; i += slice.step {
		rs.bits.Set(uint(i))
	}
}

func (s *Slice) String() string {
	return fmt.Sprintf("%d-%d", s.start, s.stop)
}

func (rs *RangeSet) Slices() []*Slice {
	result := make([]*Slice, 0)
	if rs.bits.Count() == 0 {
		return result
	}

	i, e := rs.bits.NextSet(0)
	k := i
	j := i
	for e {
		if i-j > 1 {
			result = append(result, &Slice{int(k), int(j + 1), 1, rs.padding})
			k = i
		}
		j = i
		i, e = rs.bits.NextSet(i + 1)
	}
	result = append(result, &Slice{int(k), int(j) + 1, 1, rs.padding})

	return result
}

func NewRangeSetND(args [][]string) (nd *RangeSetND, err error) {
	nd = &RangeSetND{dirty: true, ranges: make([][]*RangeSet, len(args))}

	for i, rgvec := range args {
		nd.ranges[i] = make([]*RangeSet, len(rgvec))
		for j, rg := range rgvec {
			rs, err := NewRangeSet(rg)
			if err != nil {
				return nil, err
			}

			nd.ranges[i][j] = rs
		}
	}

	return nd, nil
}

func (rs *RangeSetND) Update(other *RangeSetND) error {
	if rs.Dim() != other.Dim() {
		return fmt.Errorf("mismatched dimensions %d != %d - %w", rs.Dim(), other.Dim(), ErrInvalidRangeSet)
	}

	rs.ranges = append(rs.ranges, other.ranges...)
	rs.dirty = true
	return nil
}

func (nd *RangeSetND) Dim() int {
	if len(nd.ranges) == 0 {
		return 0
	}

	return len(nd.ranges[0])
}

func (nd *RangeSetND) Len() int {
	it := nd.Iterator()
	return it.Len()
}

func (rs *RangeSetND) Fold() {
	if !rs.dirty {
		return
	}

	dim := len(rs.ranges[0])
	vardim := 0
	dimdiff := 0
	if dim > 1 {
		for i := 0; i < dim; i++ {
			slist := map[string]struct{}{}
			for _, rs := range rs.ranges {
				slist[rs[i].String()] = struct{}{}
			}
			if len(slist) != 1 {
				dimdiff += 1
				if dimdiff > 1 {
					break
				}
				vardim = i
			}
		}
	}
	if dim == 1 || dimdiff == 1 {
		for _, set := range rs.ranges[1:] {
			rs.ranges[0][vardim].InPlaceUnion(set[vardim])
		}
		rs.ranges = [][]*RangeSet{rs.ranges[0]}
	} else {
		rs.foldMultivariate()
	}

	rs.dirty = false
}

func (nd *RangeSetND) foldMultivariate() {
	nd.foldMultivariateExpand()
	nd.Sort()

	//	fmt.Printf("%#v\n", nd.Dump())
	//	fmt.Printf("---\n")
	nd.foldMultivariateMerge()
	nd.Sort()
	//	fmt.Printf("%#v\n", nd.Dump())
	//	fmt.Printf("---\n")
}

func (nd *RangeSetND) foldMultivariateExpand() {
	maxlen := 0
	for _, rgvec := range nd.ranges {
		size := rgvec[0].Len()
		for _, rs := range rgvec[1:] {
			size *= rs.Len()
		}
		maxlen += size
	}

	// TODO use simple heuristic to make faster

	index1 := 0
	index2 := 1
	for (index1 + 1) < len(nd.ranges) {
		item1 := nd.ranges[index1]
		index2 = index1 + 1
		index1++
		for index2 < len(nd.ranges) {
			item2 := nd.ranges[index2]
			index2++
			var newItem []*RangeSet
			disjoint := false
			suppl := make([][]*RangeSet, 0)
			for pos := range item1 {
				rg1 := item1[pos]
				rg2 := item2[pos]
				rg1Intersect := rg1.Intersection(rg2)
				if rg1Intersect.Empty() {
					disjoint = true
					break
				}

				if newItem == nil {
					newItem = make([]*RangeSet, len(item1))
				}

				if rg1.Equal(rg2) {
					newItem[pos] = rg1
				} else {
					// intersection
					newItem[pos] = rg1Intersect
					// create part 1
					rg1Diff := rg1.Difference(rg2)
					if !rg1Diff.Empty() {
						i1p := make([]*RangeSet, 0, len(item1))
						i1p = append(i1p, item1[0:pos]...)
						i1p = append(i1p, rg1Diff)
						i1p = append(i1p, item1[pos+1:]...)
						suppl = append(suppl, i1p)
					}
					// create part 2
					rg2Diff := rg2.Difference(rg1)
					if !rg2Diff.Empty() {
						i2p := make([]*RangeSet, 0, len(item2))
						i2p = append(i2p, item2[0:pos]...)
						i2p = append(i2p, rg2Diff)
						i2p = append(i2p, item2[pos+1:]...)
						suppl = append(suppl, i2p)
					}
				}
			}

			if !disjoint {
				item1 = newItem
				nd.ranges[index1-1] = newItem
				index2--
				copy(nd.ranges[index2:], nd.ranges[index2+1:])
				nd.ranges = nd.ranges[:len(nd.ranges)-1]
				nd.ranges = append(nd.ranges, suppl...)
			}
		}
	}
}

func (nd *RangeSetND) foldMultivariateMerge() {
	chg := true
	for chg {
		chg = false
		index1 := 0
		index2 := 1
		for (index1 + 1) < len(nd.ranges) {
			// use 2 references on iterator to compare items by couples
			item1 := nd.ranges[index1]
			index2 = index1 + 1
			index1++
			for index2 < len(nd.ranges) {
				item2 := nd.ranges[index2]
				index2++
				newItem := make([]*RangeSet, len(item1))
				nbDiff := 0
				// compare 2 rangeset vector, item by item, the idea being
				// to merge vectors if they differ only by one item
				for pos := range item1 {
					rg1 := item1[pos]
					rg2 := item2[pos]

					rg1Intersect := rg1.Intersection(rg2)
					if rg1.Equal(rg2) {
						newItem[pos] = rg1.Clone()
					} else if rg1Intersect.Empty() { // merge on disjoint ranges
						nbDiff++
						if nbDiff > 1 {
							break
						}
						newItem[pos] = rg1.Union(rg2)
					} else if rg1.Greater(rg2) || rg1.Less(rg2) {
						nbDiff++
						if nbDiff > 1 {
							break
						}
						if rg1.Greater(rg2) {
							newItem[pos] = rg1.Clone()
						} else {
							newItem[pos] = rg2.Clone()
						}
					} else {
						// intersection but do nothing
						nbDiff = 2
						break
					}
				}
				// one change has been done: use this new item to compare
				// with other
				if nbDiff <= 1 {
					chg = true
					item1 = newItem
					nd.ranges[index1-1] = newItem
					index2--
					copy(nd.ranges[index2:], nd.ranges[index2+1:])
					nd.ranges = nd.ranges[:len(nd.ranges)-1]
				}
			}
		}
	}
}

func (nd *RangeSetND) Dump() []string {
	out := make([]string, 0, len(nd.ranges))

	for _, rgvec := range nd.ranges {
		list := []string{}
		for _, rs := range rgvec {
			list = append(list, rs.String())
		}
		out = append(out, strings.Join(list, ","))
	}

	return out
}

func (nd *RangeSetND) FormatList() [][]interface{} {
	nd.Fold()
	results := make([][]interface{}, 0, len(nd.ranges))
	for _, rgvec := range nd.ranges {
		rsets := make([]interface{}, 0, nd.Dim())
		for _, rs := range rgvec {
			if rs.Len() > 1 {
				rsets = append(rsets, fmt.Sprintf("[%s]", rs.String()))
			} else {
				rsets = append(rsets, rs.String())

			}
		}
		results = append(results, rsets)
	}

	return results
}

func (nd *RangeSetND) String() string {
	nd.Fold()
	var buffer bytes.Buffer
	for _, rgvec := range nd.ranges {
		for j, rs := range rgvec {
			buffer.WriteString(rs.String())
			if j != len(rgvec)-1 {
				buffer.WriteString("; ")
			}
		}
		buffer.WriteString("\n")
	}

	return buffer.String()
}

func (nd *RangeSetND) Ranges() [][]*RangeSet {
	return nd.ranges
}

func (nd *RangeSetND) Sort() {
	// key used for sorting purposes, based on the following
	// conditions:
	//   (1) larger vector first (#elements)
	//   (2) larger dim first  (#elements)
	//   (3) lower first index first
	//   (4) lower last index first
	sort.SliceStable(nd.ranges, func(i, j int) bool {
		isize := nd.ranges[i][0].Len()
		for _, rs := range nd.ranges[i][1:] {
			isize *= rs.Len()
		}

		jsize := nd.ranges[j][0].Len()
		for _, rs := range nd.ranges[j][1:] {
			jsize *= rs.Len()
		}

		if isize == jsize && len(nd.ranges[i]) == len(nd.ranges[j]) {
			if nd.ranges[i][0].Len() == nd.ranges[j][0].Len() {
				return nd.ranges[i][len(nd.ranges[i])-1].Len() > nd.ranges[j][len(nd.ranges[j])-1].Len()
			} else {
				return nd.ranges[i][0].Len() > nd.ranges[j][0].Len()
			}
		} else if isize == jsize {
			return len(nd.ranges[i]) > len(nd.ranges[j])
		}

		return isize > jsize
	})
}

func (nd *RangeSetND) Iterator() *RangeSetNDIterator {
	it := NewRangeSetNDIterator()

	for _, rgvec := range nd.ranges {
		slices := make([][]*RangeSetItem, nd.Dim())
		for i, rs := range rgvec {
			slices[i] = rs.Items()
		}

		it.product([]*RangeSetItem{}, slices...)
	}

	return it
}
