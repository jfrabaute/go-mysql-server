// Copyright 2020-2021 Dolthub, Inc.
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

package function

import (
	"fmt"
	"strings"

	"gopkg.in/src-d/go-errors.v1"

	"github.com/dolthub/go-mysql-server/sql"
	"github.com/dolthub/go-mysql-server/sql/expression"
)

// SRID is a function that returns SRID of Geometry object or returns a new object with altered SRID.
type SRID struct {
	expression.NaryExpression
}

var _ sql.FunctionExpression = (*SRID)(nil)

var ErrInvalidSRID = errors.NewKind("There's no spatial reference with SRID %d")

// NewSRID creates a new STX expression.
func NewSRID(args ...sql.Expression) (sql.Expression, error) {
	if len(args) != 1 && len(args) != 2 {
		return nil, sql.ErrInvalidArgumentNumber.New("ST_SRID", "1 or 2", len(args))
	}
	return &SRID{expression.NaryExpression{ChildExpressions: args}}, nil
}

// FunctionName implements sql.FunctionExpression
func (s *SRID) FunctionName() string {
	return "st_srid"
}

// Description implements sql.FunctionExpression
func (s *SRID) Description() string {
	return "returns the SRID value of given geometry object. If given a second argument, returns a new geometry object with second argument as SRID value."
}

// Type implements the sql.Expression interface.
func (s *SRID) Type() sql.Type {
	if len(s.ChildExpressions) == 1 {
		return sql.Int32
	} else {
		return s.ChildExpressions[0].Type()
	}
}

func (s *SRID) String() string {
	var args = make([]string, len(s.ChildExpressions))
	for i, arg := range s.ChildExpressions {
		args[i] = arg.String()
	}
	return fmt.Sprintf("ST_SRID(%s)", strings.Join(args, ","))
}

// WithChildren implements the Expression interface.
func (s *SRID) WithChildren(children ...sql.Expression) (sql.Expression, error) {
	return NewSRID(children...)
}

// Eval implements the sql.Expression interface.
func (s *SRID) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	// Evaluate geometry type
	g, err := s.ChildExpressions[0].Eval(ctx, row)
	if err != nil {
		return nil, err
	}

	// Return nil if geometry object is nil
	if g == nil {
		return nil, nil
	}

	// If just one argument, return X
	if len(s.ChildExpressions) == 1 {
		// Check that it is a geometry type
		switch g := g.(type) {
		case sql.Point:
			return g.SRID, nil
		case sql.Linestring:
			return g.SRID, nil
		case sql.Polygon:
			return g.SRID, nil
		default:
			return nil, sql.ErrIllegalGISValue.New(g)
		}
	}

	// Evaluate second argument
	srid, err := s.ChildExpressions[1].Eval(ctx, row)
	if err != nil {
		return nil, err
	}

	// Return null if second argument is null
	if srid == nil {
		return nil, nil
	}

	// Convert to int32
	srid, err = sql.Int32.Convert(srid)
	if err != nil {
		return nil, err
	}

	// Type assertion
	_srid := uint32(srid.(int32))

	// Must be either 0 or 4230
	if _srid != 0 && _srid != 4230 {
		return nil, ErrInvalidSRID.New(_srid)
	}

	// Create new geometry object with matching SRID
	switch g := g.(type) {
	case sql.Point:
		return sql.Point{SRID: _srid, X: g.X, Y: g.Y}, nil
	case sql.Linestring:
		return sql.Linestring{SRID: _srid, Points: g.Points}, nil
	case sql.Polygon:
		return sql.Polygon{SRID: _srid, Lines: g.Lines}, nil
	default:
		return nil, sql.ErrIllegalGISValue.New(g)
	}
}