// Package catalog reads the schema of a database from sqlite_schema and the
// schema PRAGMAs.
package catalog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/winebarrel/hazel/model"
	"github.com/winebarrel/orderedmap/v2"
)

// Read reads the tables, indexes, views and triggers of the main schema. It
// leaves out:
//
//   - names that begin with "sqlite_" in any ASCII case, which SQLite
//     reserves for itself, such as sqlite_sequence and sqlite_stat1;
//   - automatic indexes, which have no SQL and stand for a UNIQUE or PRIMARY
//     KEY constraint of their table;
//   - shadow tables, which a virtual table module creates and owns, and the
//     objects that belong to them.
//
// What PRAGMAs do not report (column collations, CHECK constraints,
// generated column expressions, constraint names, AUTOINCREMENT and
// DEFERRABLE) is left for the parser to read from the SQL. The MATCH clause
// of a foreign key is not read: SQLite parses it and keeps nothing of it, so
// PRAGMA foreign_key_list always reports NONE.
func Read(ctx context.Context, db *sql.DB) (*model.Schema, error) {
	r := &reader{ctx: ctx, db: db}
	s := model.NewSchema()
	for _, o := range r.objects() {
		r.object = o.typ + " " + o.name
		key := model.Key(o.name)
		switch o.typ {
		case "table":
			s.Tables.Set(key, r.table(o))
		case "index":
			s.Indexes.Set(key, r.index(o))
		case "view":
			s.Views.Set(key, r.view(o))
		case "trigger":
			s.Triggers.Set(key, &model.Trigger{Name: o.name, Table: o.tblName, SQL: o.sql})
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	return s, nil
}

// reader runs the queries of one Read. The first error is kept in err, and
// every query after it returns nothing. The error names the object being
// read, since a PRAGMA can fail on one object, such as a view whose table
// was dropped.
type reader struct {
	ctx    context.Context
	db     *sql.DB
	object string // "view v", or "" while reading sqlite_schema
	err    error
}

func (r *reader) fail(err error) {
	if r.object != "" {
		err = fmt.Errorf("%s: %w", r.object, err)
	}
	r.err = err
}

// queryAll runs q and scans every row into a T.
func queryAll[T any](r *reader, q string, args []any, scan func(*sql.Rows, *T) error) []T {
	if r.err != nil {
		return nil
	}
	rows, err := r.db.QueryContext(r.ctx, q, args...)
	if err != nil {
		r.fail(err)
		return nil
	}
	defer rows.Close() //nolint:errcheck
	var out []T
	for rows.Next() {
		var v T
		if err := scan(rows, &v); err != nil {
			r.fail(err)
			return nil
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		r.fail(err)
	}
	return out
}

type objectRow struct {
	typ, name, tblName, sql string
	tblType                 string // the type PRAGMA table_list reports for tblName
	withoutRowid, strict    bool
}

// objects reads sqlite_schema in creation order. The join with PRAGMA
// table_list compares names with NOCASE, which folds ASCII only, as SQLite
// does for names.
func (r *reader) objects() []objectRow {
	return queryAll(r, `
		SELECT s.type, s.name, s.tbl_name, s.sql,
		       coalesce(l.type, ''), coalesce(l.wr, 0), coalesce(l.strict, 0)
		FROM main.sqlite_schema AS s
		LEFT JOIN pragma_table_list AS l
		  ON l.schema = 'main' AND l.name = s.tbl_name COLLATE NOCASE
		WHERE s.sql IS NOT NULL
		  AND s.name NOT LIKE 'sqlite\_%' ESCAPE '\'
		  AND coalesce(l.type, '') <> 'shadow'
		ORDER BY s.rowid`, nil,
		func(rows *sql.Rows, o *objectRow) error {
			return rows.Scan(&o.typ, &o.name, &o.tblName, &o.sql, &o.tblType, &o.withoutRowid, &o.strict)
		})
}

type columnRow struct {
	name, typ string
	notNull   bool
	dflt      sql.NullString
	pk        int
	hidden    int
}

func (r *reader) table(o objectRow) *model.Table {
	t := &model.Table{
		Name:         o.name,
		SQL:          o.sql,
		Virtual:      o.tblType == "virtual",
		Strict:       o.strict,
		WithoutRowid: o.withoutRowid,
		Columns:      orderedmap.New[string, *model.Column](),
	}
	if t.Virtual {
		return t
	}

	cols := queryAll(r, `
		SELECT name, type, "notnull", dflt_value, pk, hidden
		FROM pragma_table_xinfo(?, 'main') ORDER BY cid`, []any{o.name},
		func(rows *sql.Rows, c *columnRow) error {
			return rows.Scan(&c.name, &c.typ, &c.notNull, &c.dflt, &c.pk, &c.hidden)
		})
	pkCols := make([]string, len(cols))
	nPK := 0
	for _, c := range cols {
		col := &model.Column{Name: c.name, Type: c.typ, NotNull: c.notNull}
		if c.dflt.Valid {
			col.Default = &c.dflt.String
		}
		switch c.hidden {
		case 2:
			col.Generated = model.GeneratedVirtual
		case 3:
			col.Generated = model.GeneratedStored
		}
		t.Columns.Set(model.Key(c.name), col)
		if c.pk > 0 {
			pkCols[c.pk-1] = c.name
			nPK++
		}
	}

	t.ForeignKeys = r.foreignKeys(o.name)

	// The automatic indexes are in sqlite_schema, without SQL, in the order
	// the constraints were written in. PRAGMA index_list does not keep that
	// order: it moves an ON CONFLICT REPLACE index to the end. The PRIMARY
	// KEY of a WITHOUT ROWID table is the table itself and has no row there;
	// its NULL rowid sorts first.
	indexes := queryAll(r, `
		SELECT l.name, l.origin FROM pragma_index_list(?, 'main') AS l
		LEFT JOIN main.sqlite_schema AS s ON s.name = l.name
		WHERE l.origin IN ('pk', 'u') ORDER BY s.rowid`, []any{o.name},
		func(rows *sql.Rows, i *[2]string) error {
			return rows.Scan(&i[0], &i[1])
		})
	for _, i := range indexes {
		keys := r.indexColumns(i[0])
		if i[1] == "pk" {
			t.PrimaryKey = keys
		} else {
			t.Uniques = append(t.Uniques, &model.Unique{Columns: keys})
		}
	}
	// An INTEGER PRIMARY KEY is the rowid and has no index, so its column
	// comes from table_xinfo.
	if t.PrimaryKey == nil {
		for _, name := range pkCols[:nPK] {
			t.PrimaryKey = append(t.PrimaryKey, &model.IndexColumn{Name: name, Collation: "BINARY"})
		}
	}
	return t
}

type fkRow struct {
	id                 int
	table, from        string
	to                 sql.NullString
	onUpdate, onDelete string
}

func (r *reader) foreignKeys(table string) []*model.ForeignKey {
	// The ids count from the last constraint in the CREATE TABLE, so the
	// descending order gives the order the constraints were written in.
	rows := queryAll(r, `
		SELECT id, "table", "from", "to", on_update, on_delete
		FROM pragma_foreign_key_list(?, 'main') ORDER BY id DESC, seq`, []any{table},
		func(rows *sql.Rows, f *fkRow) error {
			return rows.Scan(&f.id, &f.table, &f.from, &f.to, &f.onUpdate, &f.onDelete)
		})
	var fks []*model.ForeignKey
	var fk *model.ForeignKey
	for i, row := range rows {
		if i == 0 || row.id != rows[i-1].id {
			fk = &model.ForeignKey{RefTable: row.table, OnUpdate: row.onUpdate, OnDelete: row.onDelete}
			fks = append(fks, fk)
		}
		fk.Columns = append(fk.Columns, row.from)
		fk.RefColumns = append(fk.RefColumns, row.to.String)
	}
	return fks
}

type indexColumnRow struct {
	name sql.NullString
	desc bool
	coll string
}

// indexColumns reads the keys of an index. The rowid and the other columns
// that SQLite appends to an index are not keys and are left out.
func (r *reader) indexColumns(index string) []*model.IndexColumn {
	rows := queryAll(r, `
		SELECT name, "desc", coll FROM pragma_index_xinfo(?, 'main')
		WHERE "key" = 1 ORDER BY seqno`, []any{index},
		func(rows *sql.Rows, c *indexColumnRow) error {
			return rows.Scan(&c.name, &c.desc, &c.coll)
		})
	cols := make([]*model.IndexColumn, len(rows))
	for i, row := range rows {
		cols[i] = &model.IndexColumn{Name: row.name.String, Desc: row.desc, Collation: row.coll}
	}
	return cols
}

func (r *reader) index(o objectRow) *model.Index {
	idx := &model.Index{Name: o.name, Table: o.tblName, SQL: o.sql}
	flags := queryAll(r, `
		SELECT "unique", partial FROM pragma_index_list(?, 'main') WHERE name = ?`, []any{o.tblName, o.name},
		func(rows *sql.Rows, f *[2]bool) error {
			return rows.Scan(&f[0], &f[1])
		})
	for _, f := range flags {
		idx.Unique, idx.Partial = f[0], f[1]
	}
	idx.Columns = r.indexColumns(o.name)
	return idx
}

func (r *reader) view(o objectRow) *model.View {
	cols := queryAll(r, `SELECT name FROM pragma_table_info(?, 'main') ORDER BY cid`, []any{o.name},
		func(rows *sql.Rows, name *string) error {
			return rows.Scan(name)
		})
	return &model.View{Name: o.name, SQL: o.sql, Columns: cols}
}
