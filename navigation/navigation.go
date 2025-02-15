/*
Copyright (C) 2016-2017 dapperdox.com

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.

*/

// Package navigation provides structure for holding API navigation.
package navigation

// Node represents the relationship between pages.
type Node struct {
	ChildMap  map[string]*Node
	Children  []*Node
	SortOrder string
	Name      string
	ID        string
	URI       string
}

// ByOrder implements the Sorter interface for array of Node.
type ByOrder []*Node

func (n ByOrder) Len() int {
	return len(n)
}

func (n ByOrder) Less(a, b int) bool {
	return n[a].SortOrder < n[b].SortOrder
}

func (n ByOrder) Swap(a, b int) {
	n[a], n[b] = n[b], n[a]
}
