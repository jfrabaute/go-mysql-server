// Copyright 2021 Dolthub, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plan

import (
	"io"
	"sync"

	"github.com/dolthub/go-mysql-server/sql"
)

// NewCachedResults returns a cached results plan Node, which will use a
// RowCache to cache results generated by Child.RowIter() and return those
// results for future calls to RowIter. This node is only safe to use if the
// Child is determinstic and is not dependent on the |row| parameter in the
// call to RowIter.
func NewCachedResults(n sql.Node) *CachedResults {
	return &CachedResults{UnaryNode: UnaryNode{n}}
}

type CachedResults struct {
	UnaryNode
	cache   sql.RowsCache
	dispose sql.DisposeFunc
	mutex   sync.Mutex
	noCache bool
}

func (n *CachedResults) RowIter(ctx *sql.Context, r sql.Row) (sql.RowIter, error) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	if n.cache != nil {
		return sql.RowsToRowIter(n.cache.Get()...), nil
	} else if n.noCache {
		return n.UnaryNode.Child.RowIter(ctx, r)
	}
	ci, err := n.UnaryNode.Child.RowIter(ctx, r)
	if err != nil {
		return nil, err
	}
	cache, dispose := ctx.Memory.NewRowsCache()
	return &cachedResultsIter{n, ci, cache, dispose}, nil
}

func (n *CachedResults) Dispose() {
	if n.dispose != nil {
		n.dispose()
	}
}

func (n *CachedResults) String() string {
	pr := sql.NewTreePrinter()
	_ = pr.WriteNode("CachedResults")
	_ = pr.WriteChildren(n.UnaryNode.Child.String())
	return pr.String()
}

func (n *CachedResults) DebugString() string {
	pr := sql.NewTreePrinter()
	_ = pr.WriteNode("CachedResults")
	_ = pr.WriteChildren(sql.DebugString(n.UnaryNode.Child))
	return pr.String()
}

func (n *CachedResults) WithChildren(children ...sql.Node) (sql.Node, error) {
	if len(children) != 1 {
		return nil, sql.ErrInvalidChildrenNumber.New(n, len(children), 1)
	}
	nn := *n
	nn.UnaryNode.Child = children[0]
	return &nn, nil
}

// CheckPrivileges implements the interface sql.Node.
func (n *CachedResults) CheckPrivileges(ctx *sql.Context, opChecker sql.PrivilegedOperationChecker) bool {
	return n.Child.CheckPrivileges(ctx, opChecker)
}

func (n *CachedResults) getCachedResults() []sql.Row {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	if n.cache == nil {
		return nil
	}
	return n.cache.Get()
}

type cachedResultsIter struct {
	parent  *CachedResults
	iter    sql.RowIter
	cache   sql.RowsCache
	dispose sql.DisposeFunc
}

func (i *cachedResultsIter) Next(ctx *sql.Context) (sql.Row, error) {
	r, err := i.iter.Next(ctx)
	if i.cache != nil {
		if err != nil {
			if err == io.EOF {
				i.parent.mutex.Lock()
				defer i.parent.mutex.Unlock()
				i.setCacheInParent()
			} else {
				i.cleanUp()
			}
		} else {
			aerr := i.cache.Add(r)
			if aerr != nil {
				i.cleanUp()
				i.parent.mutex.Lock()
				defer i.parent.mutex.Unlock()
				i.parent.noCache = true
			}
		}
	}
	return r, err
}

func (i *cachedResultsIter) setCacheInParent() {
	if i.parent.cache == nil {
		i.parent.cache = i.cache
		i.parent.dispose = i.dispose
		i.cache = nil
		i.dispose = nil
	} else {
		i.cleanUp()
	}
}

func (i *cachedResultsIter) cleanUp() {
	if i.dispose != nil {
		i.dispose()
		i.cache = nil
		i.dispose = nil
	}
}

func (i *cachedResultsIter) Close(ctx *sql.Context) error {
	i.cleanUp()
	return i.iter.Close(ctx)
}
