package pagination

import (
	"fmt"
	"reflect"
	"strings"
)

// LikeEscapeChar is the character EscapeLike prefixes to LIKE metacharacters.
// Backslash is deliberately avoided: MySQL also parses it inside string
// literals, so ESCAPE '\' is a syntax error there while Postgres and SQLite
// reject the doubled form MySQL requires. This character needs no quoting in
// any dialect.
const LikeEscapeChar = "!"

// LikeEscapeClause declares LikeEscapeChar to the database. The Like and ILike
// operators append it automatically; callers that assemble raw LIKE clauses
// with EscapeLike must append it themselves, otherwise the escaping is
// silently ignored by every dialect whose default escape character differs.
const LikeEscapeClause = `ESCAPE '` + LikeEscapeChar + `'`

var likeEscaper = strings.NewReplacer(
	LikeEscapeChar, LikeEscapeChar+LikeEscapeChar,
	"%", LikeEscapeChar+"%",
	"_", LikeEscapeChar+"_",
)

// EscapeLike escapes LIKE/ILIKE wildcards so user input is matched literally
// inside the surrounding %...%. It is applied automatically to Like/ILike
// filters and is exported for callers that assemble raw LIKE clauses outside
// the FilterBuilder, which must pair it with LikeEscapeClause
//
//	db.Where("name ILIKE ? "+pagination.LikeEscapeClause,
//	    "%"+pagination.EscapeLike(term)+"%",
//	)
func EscapeLike(v any) string {
	return likeEscaper.Replace(fmt.Sprintf("%v", v))
}

// derefVal unwraps pointer values so filters always store the
// underlying value. Nil pointers become nil.
func derefVal(v any) any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		return rv.Elem().Interface()
	}
	return v
}
