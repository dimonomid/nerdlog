package main

import (
	"strings"

	"github.com/juju/errors"
)

var DefaultSelectQuery SelectQuery = FieldNameTime + " STICKY, " + FieldNameMessage + ", " + FieldNameLStream + ", *"

const (
	FieldNameTime      = "time"
	FieldNameMessage   = "message"
	FieldNameLStream   = "lstream"
	FieldNameLogFile   = "logfile"
	FieldNameLogLineno = "loglineno"
)

var FieldNamesSpecial = map[string]struct{}{
	FieldNameTime:      {},
	FieldNameMessage:   {},
	FieldNameLStream:   {},
	FieldNameLogFile:   {},
	FieldNameLogLineno: {},
}

// TODO explain
type SelectQuery string

type SelectQueryParsed struct {
	Fields []SelectQueryField

	// If IncludeAll is true, then after all the fields in the Fields slice, all other
	// fields will be included in lexicographical order.
	IncludeAll bool
}

type SelectQueryField struct {
	// Name is the actual field name as is in logs.
	Name string

	// DisplayName is how it's displayed in the table header. Often it's the same
	// as Name.
	DisplayName string

	// If Sticky is true, the column will always be visible.
	Sticky bool
}

func ParseSelectQuery(sq SelectQuery) (*SelectQueryParsed, error) {
	ret := &SelectQueryParsed{}

	if sq == "" {
		return nil, errors.Errorf("no fields selected")
	}

	fields := strings.Split(string(sq), ",")

	for i, fldStr := range fields {
		parts := strings.Fields(fldStr)

		if len(parts) == 0 {
			return nil, errors.Errorf("empty field #%d", i)
		}

		if parts[0] == "*" {
			if len(parts) != 1 {
				return nil, errors.Errorf("invalid wildcard specifier")
			}

			ret.IncludeAll = true
			continue
		}

		if ret.IncludeAll {
			return nil, errors.Errorf("wildcard can only be the last item")
		}

		field := SelectQueryField{
			Name:        parts[0],
			DisplayName: parts[0],
		}

		const (
			stateRoot = iota
			stateWaitDisplayName
		)

		state := stateRoot
		for _, token := range parts[1:] {
			switch state {
			case stateRoot:
				switch strings.ToLower(token) {
				case "as":
					if field.Name != field.DisplayName {
						return nil, errors.Errorf("syntax error for field %s: more than a single 'AS'", field.Name)
					}

					state = stateWaitDisplayName
					continue
				case "sticky":
					if field.Sticky {
						return nil, errors.Errorf("syntax error for field %s: more than a single 'STICKY'", field.Name)
					}

					field.Sticky = true
					continue
				default:
					return nil, errors.Errorf("syntax error for field %s", field.Name)
				}

			case stateWaitDisplayName:
				field.DisplayName = token
				state = stateRoot
			}
		}

		if state != stateRoot {
			return nil, errors.Errorf("incomplete field %s", field.Name)
		}

		ret.Fields = append(ret.Fields, field)
	}

	return ret, nil
}

func (sqp *SelectQueryParsed) Marshal() SelectQuery {
	var sb strings.Builder

	add := func(i int, s string) {
		if i != 0 {
			sb.WriteString(", ")
		}

		sb.WriteString(s)
	}

	var n int
	for i, fld := range sqp.Fields {
		v := fld.Name

		if fld.DisplayName != fld.Name {
			v += " AS " + fld.DisplayName
		}

		if fld.Sticky {
			v += " STICKY"
		}

		add(i, v)
		n = i
	}

	if sqp.IncludeAll {
		add(n, "*")
		n++
	}

	return SelectQuery(sb.String())
}
