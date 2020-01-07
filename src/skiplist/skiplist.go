// Copyright 2012 Google Inc. All rights reserved.
// Author: Ric Szopa (Ryszard) <ryszard.szopa@gmail.com>

// Package skiplist implements skip list based maps and sets.
//
// Skip lists are a data structure that can be used in place of
// balanced trees. Skip lists use probabilistic balancing rather than
// strictly enforced balancing and as a result the algorithms for
// insertion and deletion in skip lists are much simpler and
// significantly faster than equivalent algorithms for balanced trees.
//
// Skip lists were first described in Pugh, William (June 1990). "Skip
// lists: a probabilistic alternative to balanced
// trees". Communications of the ACM 33 (6): 668–676
package skiplist

import (
	"math/rand"
)

// p is the fraction of nodes with level i pointers that also have
// level i+1 pointers. p equal to 1/4 is a good value from the point
// of view of speed and space requirements. If variability of running
// times is a concern, 1/2 is a better value for p.
const p = 0.25

const DefaultMaxLevel = 32

// A node is a container for key-value pairs that are stored in a skip
// list.
type node struct {
	forward    []*node
	backward   *node
	key, value interface{}
}

// next returns the next node in the skip list containing n.
func (n *node) next() *node {
	if len(n.forward) == 0 {
		return nil
	}
	return n.forward[0]
}

// previous returns the previous node in the skip list containing n.
func (n *node) previous() *node {
	return n.backward
}

// hasNext returns true if n has a next node.
func (n *node) hasNext() bool {
	return n.next() != nil
}

// hasPrevious returns true if n has a previous node.
func (n *node) hasPrevious() bool {
	return n.previous() != nil
}

// A SkipList is a map-like data structure that maintains an ordered
// collection of key-value pairs. Insertion, lookup, and deletion are
// all O(log n) operations. A SkipList can efficiently store up to
// 2^MaxLevel items.
//
// To iterate over a skip list (where s is a
// *SkipList):
//
//	for i := s.Iterator(); i.Next(); {
//		// do something with i.Key() and i.Value()
//	}
type SkipList struct {
	lessThan func(l, r interface{}) bool
	header   *node
	footer   *node
	length   int
	update   []*node
}

// Len returns the length of s.
func (s *SkipList) Len() int {
	return s.length
}

// Iterator is an interface that you can use to iterate through the
// skip list (in its entirety or fragments). For an use example, see
// the documentation of SkipList.
//
// Key and Value return the key and the value of the current node.
type Iterator interface {
	// Next returns true if the iterator contains subsequent elements
	// and advances its state to the next element if that is possible.
	Next() (ok bool)
	// Previous returns true if the iterator contains previous elements
	// and rewinds its state to the previous element if that is possible.
	Previous() (ok bool)
	// Key returns the current key.
	Key() interface{}
	// Value returns the current value.
	Value() interface{}
	// Seek reduces iterative seek costs for searching forward into the Skip List
	// by remarking the range of keys over which it has scanned before.  If the
	// requested key occurs prior to the point, the Skip List will start searching
	// as a safeguard.  It returns true if the key is within the known range of
	// the list.
	Seek(key interface{}) (ok bool)
	// Close this iterator to reap resources associated with it.  While not
	// strictly required, it will provide extra hints for the garbage collector.
	Close()
}

type iter struct {
	current *node
	key     interface{}
	list    *SkipList
	value   interface{}
}

func (i iter) Key() interface{} {
	return i.key
}

func (i iter) Value() interface{} {
	return i.value
}

func (i *iter) Next() bool {
	if !i.current.hasNext() {
		return false
	}

	i.current = i.current.next()
	i.key = i.current.key
	i.value = i.current.value

	return true
}

func (i *iter) Previous() bool {
	if !i.current.hasPrevious() {
		return false
	}

	i.current = i.current.previous()
	i.key = i.current.key
	i.value = i.current.value

	return true
}

func (i *iter) Seek(key interface{}) (ok bool) {
	current := i.current
	list := i.list

	// If the existing iterator outside of the known key range, we should set the
	// position back to the beginning of the list.
	if current == nil {
		current = list.header
	}

	// If the target key occurs before the current key, we cannot take advantage
	// of the heretofore spent traversal cost to find it; resetting back to the
	// beginning is the safest choice.
	if current.key != nil && list.lessThan(key, current.key) {
		current = list.header
	}

	// We should back up to the so that we can seek to our present value if that
	// is requested for whatever reason.
	if current.backward == nil {
		current = list.header
	} else {
		current = current.backward
	}

	current = list.getPath(current, nil, key)

	if current == nil {
		return
	}

	i.current = current
	i.key = current.key
	i.value = current.value

	return true
}

func (i *iter) Close() {
	i.key = nil
	i.value = nil
	i.current = nil
	i.list = nil
}

type rangeIterator struct {
	iter
	upperLimit interface{}
	lowerLimit interface{}
}

func (i *rangeIterator) Next() bool {
	if !i.current.hasNext() {
		return false
	}

	next := i.current.next()

	if !i.list.lessThan(next.key, i.upperLimit) {
		return false
	}

	i.current = i.current.next()
	i.key = i.current.key
	i.value = i.current.value
	return true
}

func (i *rangeIterator) Previous() bool {
	if !i.current.hasPrevious() {
		return false
	}

	previous := i.current.previous()

	if i.list.lessThan(previous.key, i.lowerLimit) {
		return false
	}

	i.current = i.current.previous()
	i.key = i.current.key
	i.value = i.current.value
	return true
}

func (i *rangeIterator) Seek(key interface{}) (ok bool) {
	if i.list.lessThan(key, i.lowerLimit) {
		return
	} else if !i.list.lessThan(key, i.upperLimit) {
		return
	}

	return i.iter.Seek(key)
}

func (i *rangeIterator) Close() {
	i.iter.Close()
	i.upperLimit = nil
	i.lowerLimit = nil
}

// Iterator returns an Iterator that will go through all elements s.
func (s *SkipList) Iterator() Iterator {
	return &iter{
		current: s.header,
		list:    s,
	}
}

// Seek returns a bidirectional iterator starting with the first element whose
// key is greater or equal to key; otherwise, a nil iterator is returned.
func (s *SkipList) Seek(key interface{}) Iterator {
	current := s.getPath(s.header, nil, key)
	if current == nil {
		return nil
	}

	return &iter{
		current: current,
		key:     current.key,
		list:    s,
		value:   current.value,
	}
}

// SeekToFirst returns a bidirectional iterator starting from the first element
// in the list if the list is populated; otherwise, a nil iterator is returned.
func (s *SkipList) SeekToFirst() Iterator {
	if s.length == 0 {
		return nil
	}

	current := s.header.next()

	return &iter{
		current: current,
		key:     current.key,
		list:    s,
		value:   current.value,
	}
}

// SeekToLast returns a bidirectional iterator starting from the last element
// in the list if the list is populated; otherwise, a nil iterator is returned.
func (s *SkipList) SeekToLast() Iterator {
	current := s.footer
	if current == nil {
		return nil
	}

	return &iter{
		current: current,
		key:     current.key,
		list:    s,
		value:   current.value,
	}
}

// Range returns an iterator that will go through all the
// elements of the skip list that are greater or equal than from, but
// less than to.
func (s *SkipList) Range(from, to interface{}) Iterator {
	start := s.getPath(s.header, nil, from)
	return &rangeIterator{
		iter: iter{
			current: &node{
				forward:  []*node{start},
				backward: start,
			},
			list: s,
		},
		upperLimit: to,
		lowerLimit: from,
	}
}

func (s *SkipList) level() int {
	return len(s.header.forward) - 1
}

func maxInt(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func (s *SkipList) effectiveMaxLevel() int {
	return maxInt(s.level(), DefaultMaxLevel)
}

// Returns a new random level.
func (s SkipList) randomLevel() (n int) {
	for n = 0; n < s.effectiveMaxLevel() && rand.Float64() < p; n++ {
	}
	return
}

// Get returns the value associated with key from s (nil if the key is
// not present in s). The second return value is true when the key is
// present.
func (s *SkipList) Get(key interface{}) (value interface{}, ok bool) {
	candidate := s.getPath(s.header, nil, key)

	if candidate == nil || candidate.key != key {
		return nil, false
	}

	return candidate.value, true
}

// GetGreaterOrEqual finds the node whose key is greater than or equal
// to min. It returns its value, its actual key, and whether such a
// node is present in the skip list.
func (s *SkipList) GetGreaterOrEqual(min interface{}) (actualKey, value interface{}, ok bool) {
	candidate := s.getPath(s.header, nil, min)

	if candidate != nil {
		return candidate.key, candidate.value, true
	}
	return nil, nil, false
}

// getPath populates update with nodes that constitute the path to the
// node that may contain key. The candidate node will be returned. If
// update is nil, it will be left alone (the candidate node will still
// be returned). If update is not nil, but it doesn't have enough
// slots for all the nodes in the path, getPath will panic.
func (s *SkipList) getPath(current *node, update []*node, key interface{}) *node {
	depth := len(current.forward) - 1

	for i := depth; i >= 0; i-- {
		for current.forward[i] != nil && s.lessThan(current.forward[i].key, key) {
			current = current.forward[i]
		}
		if update != nil {
			update[i] = current
		}
	}
	return current.next()
}

// Sets set the value associated with key in s.
func (s *SkipList) Set(key, value interface{}) {
	if key == nil {
		return
	}
	// s.level starts from 0, so we need to allocate one.
	update := s.update[:s.level()+1]
	candidate := s.getPath(s.header, update, key)

	if candidate != nil && candidate.key == key {
		candidate.value = value
		return
	}

	newLevel := s.randomLevel()

	if currentLevel := s.level(); newLevel > currentLevel {
		// there are no pointers for the higher levels in
		// update. Header should be there. Also add higher
		// level links to the header.
		for i := currentLevel + 1; i <= newLevel; i++ {
			update = append(update, s.header)
			s.header.forward = append(s.header.forward, nil)
		}
	}

	newNode := &node{
		forward: make([]*node, newLevel+1, s.effectiveMaxLevel()+1),
		key:     key,
		value:   value,
	}

	if previous := update[0]; previous.key != nil {
		newNode.backward = previous
	}

	for i := 0; i <= newLevel; i++ {
		newNode.forward[i] = update[i].forward[i]
		update[i].forward[i] = newNode
	}

	s.length++

	if newNode.forward[0] != nil {
		if newNode.forward[0].backward != newNode {
			newNode.forward[0].backward = newNode
		}
	}

	if s.footer == nil || s.lessThan(s.footer.key, key) {
		s.footer = newNode
	}
}

// Delete removes the node with the given key.
//
// It returns the old value and whether the node was present.
func (s *SkipList) Delete(key interface{}) (value interface{}, ok bool) {
	if key == nil {
		return nil, false
	}
	update := s.update[:s.level()+1]
	candidate := s.getPath(s.header, update, key)

	if candidate == nil || candidate.key != key {
		return nil, false
	}

	previous := candidate.backward
	if s.footer == candidate {
		s.footer = previous
	}

	next := candidate.next()
	if next != nil {
		next.backward = previous
	}

	for i := 0; i <= s.level() && update[i].forward[i] == candidate; i++ {
		update[i].forward[i] = candidate.forward[i]
	}

	for s.level() > 0 && s.header.forward[s.level()] == nil {
		s.header.forward = s.header.forward[:s.level()]
	}
	s.length--

	return candidate.value, true
}

// NewCustomMap returns a new SkipList that will use lessThan as the
// comparison function. lessThan should define a linear order on keys
// you intend to use with the SkipList.
func NewCustomMap(lessThan func(l, r interface{}) bool) *SkipList {
	return &SkipList{
		lessThan: lessThan,
		header: &node{
			forward: []*node{nil},
		},
		update: make([]*node, DefaultMaxLevel+1),
	}
}

// NewIntKey returns a SkipList that accepts int keys.
func NewIntMap() *SkipList {
	return NewCustomMap(func(l, r interface{}) bool {
		return l.(int) < r.(int)
	})
}

// NewIntKey returns a SkipList that accepts int keys.
func NewInt32Map() *SkipList {
	return NewCustomMap(func(l, r interface{}) bool {
		return l.(int32) < r.(int32)
	})
}

// NewStringMap returns a SkipList that accepts string keys.
func NewStringMap() *SkipList {
	return NewCustomMap(func(l, r interface{}) bool {
		return l.(string) < r.(string)
	})
}