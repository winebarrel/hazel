# The real-world schema tests: schemas that projects on GitHub ship for SQLite,
# loaded into an empty database and checked to round-trip through hazel dump
# and hazel plan. Included by the Makefile. See REALWORLD-TESTS.md.
#
# One record per line: name|owner/repo|commit|path of the schema file.
#
# Every source is pinned to a commit, not a branch, so that an upstream schema
# change cannot turn CI red on its own. To move a schema to a newer upstream
# commit, resolve it and replace the SHA:
#   git ls-remote https://github.com/<owner>/<repo> HEAD
# then run `make test-realworld REALWORLD=<name>`.
define REALWORLD_SCHEMAS
anki|ankitects/anki|827f4aa42681f1d60ab0fe4ad76eb75d767a385e|rslib/src/storage/schema11.sql
chinook|lerocha/chinook-database|7f67772503d71ba90f19283c38e93923addb43fa|ChinookDatabase/DataSources/Chinook_Sqlite.sql
ejabberd|processone/ejabberd|b33f2cc64f32f3e43378faa86297dcacdeec58f2|sql/lite.new.sql
freeradius|FreeRADIUS/freeradius-server|bbb53122d9c275563ced1dd4e29f9c0aae699142|raddb/mods-config/sql/main/sqlite/schema.sql
mediawiki|wikimedia/mediawiki|377002822b34077904f4e83c21faaf69d1bfea9a|sql/sqlite/tables-generated.sql
memos|usememos/memos|509e41bd1a1b2882404ad804ce2f2954cfad236a|store/migration/sqlite/LATEST.sql
pihole|pi-hole/pi-hole|f7c0220e63e753c664a1ec08d42d6b647b86005d|advanced/Templates/gravity.db.sql
powerdns|PowerDNS/pdns|08c08d0579f58c0ddaf4d10e074856092c81061b|modules/gsqlite3backend/schema.sqlite3.sql
roundcube|roundcube/roundcubemail|058b11d0535af2007af825c42f29f745e97b8f0b|SQL/sqlite.initial.sql
sakila|jOOQ/sakila|e089a5b1ec9af0df7a9c6a5d47d49fa1736a4e84|sqlite-sakila-db/sqlite-sakila-schema.sql
temporal|temporalio/temporal|170fb83dca48fe82d69aa3ec3b6fe25acfc1791b|schema/sqlite/v3/temporal/schema.sql
zotero|zotero/zotero|4949f7459e73d6e0f5e24be412a692cf29a848dc|resource/schema/userdata.sql
endef
export REALWORLD_LIST := $(REALWORLD_SCHEMAS)

# Prints the list, one record per line, for test/realworld/run.sh.
.PHONY: print-realworld
print-realworld:
	@printf '%s\n' "$$REALWORLD_LIST"

# REALWORLD=a,b checks only the named schemas. REALWORLD_CACHE=DIR keeps the
# downloads in DIR.
.PHONY: test-realworld
test-realworld:
	test/realworld/run.sh
