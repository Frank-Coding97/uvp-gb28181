"""Execute permission catalog migrations against existing authorization fixtures."""
import json
import re
import sqlite3
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATABASE = ROOT / "server/resource/database"
CATALOG = DATABASE / "gb28181/button-permissions.json"
VERSION = "2026-09-05-button-permission-catalog"


class ButtonCatalogTest(unittest.TestCase):
    def setUp(self):
        self.catalog = json.loads(CATALOG.read_text())

    def test_catalog_has_unique_codes_and_source_evidence(self):
        buttons = self.catalog["buttons"]
        self.assertGreater(len(buttons), 98)
        codes = [row["permission"] for row in buttons]
        self.assertEqual(len(codes), len(set(codes)))
        for row in buttons:
            self.assertTrue(row["parentPath"], row)
            self.assertTrue(row["source"], row)
            for source in row["source"]:
                self.assertTrue((ROOT / source.split(":")[0]).is_file(), source)
            if not row["apis"]:
                self.assertTrue(row.get("notes"), row)
            for api in row["apis"]:
                self.assertIn(api["method"], ["GET", "POST", "PUT", "PATCH", "DELETE"])
                self.assertTrue(api["path"].startswith("/api/"), api)

    def test_frontend_permission_literals_are_registered(self):
        codes = {b["permission"] for b in self.catalog["buttons"]}
        codes.update(self.catalog.get("pagePermissions", []))
        for path in (ROOT / "web/src").rglob("*"):
            if path.suffix not in [".vue", ".ts", ".tsx"] or ".test." in path.name or "/mock/" in str(path):
                continue
            literals = re.findall(r'''['"]((?:gb28181|system|plugins):[A-Za-z0-9_:-]+)['"]''', path.read_text())
            self.assertFalse(set(literals) - codes, f"{path}: {set(literals) - codes}")

    def test_catalog_apis_are_registered_routes(self):
        routes = set()
        files = ["server/app/routes/routes.go", "server/app/gb28181/routes/routes.go", "server/plugins/example/routes/exampleroutes.go"]
        for filename in files:
            groups = {"engine": "", "protected": "/api", "public": "/api", "zlm": "/api/gb28181/zlm"}
            for line in (ROOT / filename).read_text().splitlines():
                group = re.search(r'(\w+)\s*:=\s*(\w+)\.Group\("([^"]*)"\)', line)
                if group and group[2] in groups:
                    groups[group[1]] = groups[group[2]] + group[3]
                route = re.search(r'(\w+)\.(GET|POST|PUT|PATCH|DELETE)\("([^"]*)"', line)
                if route and route[1] in groups:
                    routes.add((route[2], groups[route[1]] + route[3]))
        for button in self.catalog["buttons"]:
            for api in button["apis"]:
                self.assertIn((api["method"], api["path"]), routes, button["permission"])

    def database(self):
        db = sqlite3.connect(":memory:")
        db.executescript("""
        CREATE TABLE sys_menu(id INTEGER PRIMARY KEY AUTOINCREMENT,parent_id INTEGER,
          path TEXT,name TEXT,component TEXT,title TEXT,hide INTEGER,disable INTEGER,
          sort INTEGER,type INTEGER,permission TEXT,icon TEXT,created_at TEXT,
          updated_at TEXT,created_by INTEGER,deleted_at TEXT);
        CREATE TABLE sys_api(id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,path TEXT,
          method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT);
        CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER,PRIMARY KEY(menu_id,api_id));
        CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER);
        CREATE TABLE sys_casbin_rule(ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT);
        INSERT INTO sys_menu(id,parent_id,path,type,permission) VALUES(1,0,'/system',1,''),
          (2,0,'/unrelated',2,''),(3,2,'',3,'unrelated:edit');
        INSERT INTO sys_api(id,title,path,method) VALUES(1,'old','/api/unrelated','PUT');
        INSERT INTO sys_menu_api VALUES(3,1);
        INSERT INTO sys_role_menu VALUES(1,3),(2,3);
        INSERT INTO sys_casbin_rule VALUES('p','role_2','/api/unrelated','PUT','*','','');
        """)
        paths = {b["parentPath"] for b in self.catalog["buttons"]}
        # Parent pages exist in a normal installation; dormant pages are seeded by this migration.
        dormant = {p["path"] for p in self.catalog.get("dormantPages", [])}
        for path in sorted(paths - dormant):
            db.execute("INSERT INTO sys_menu(parent_id,path,type,permission,disable) VALUES(0,?,2,'',0)", (path,))
        # Existing button and soft-deleted collision must not be conflated.
        first = self.catalog["buttons"][0]
        db.execute("INSERT INTO sys_menu(parent_id,path,type,permission,title) VALUES(2,'',3,?,'custom-title')", (first["permission"],))
        old_id = db.execute("SELECT last_insert_rowid()").fetchone()[0]
        db.execute("INSERT INTO sys_role_menu VALUES(2,?)", (old_id,))
        last = self.catalog["buttons"][-1]
        db.execute("INSERT INTO sys_menu(parent_id,path,type,permission,deleted_at) VALUES(2,'',3,?,'2020-01-01')", (last["permission"],))
        api = next(b["apis"][0] for b in self.catalog["buttons"] if b["apis"])
        db.execute("INSERT INTO sys_api(path,method,deleted_at) VALUES(?,?,'2020-01-01')", (api["path"], api["method"]))
        return db, first["permission"], old_id

    @staticmethod
    def snapshot(db, table):
        return db.execute(f"SELECT * FROM {table} ORDER BY 1,2").fetchall()

    def test_migrations_are_idempotent_and_preserve_grants(self):
        for suffix in ["", "-postgresql", "-sqlserver"]:
            with self.subTest(dialect=suffix):
                sql = (DATABASE / "gb28181/migrations" / f"{VERSION}{suffix}.sql").read_text()
                # SQL Server Unicode literals are normalized only for this SQLite semantic test.
                executable = re.sub(r"\bN'", "'", sql)
                db, existing_code, old_id = self.database()
                grants = {t: self.snapshot(db, t) for t in ["sys_role_menu", "sys_casbin_rule"]}
                db.executescript(executable)
                once = {t: self.snapshot(db, t) for t in ["sys_menu", "sys_api", "sys_menu_api"]}
                db.executescript(executable)
                for table, rows in once.items():
                    self.assertEqual(rows, self.snapshot(db, table), table)
                for table, rows in grants.items():
                    self.assertEqual(rows, self.snapshot(db, table), table)
                self.assertEqual([(3, 1)], db.execute("SELECT * FROM sys_menu_api WHERE menu_id=3").fetchall())
                self.assertEqual((old_id, "custom-title"), db.execute("SELECT id,title FROM sys_menu WHERE permission=? AND deleted_at IS NULL", (existing_code,)).fetchone())
                for b in self.catalog["buttons"]:
                    rows = db.execute("SELECT id,parent_id FROM sys_menu WHERE permission=? AND type=3 AND deleted_at IS NULL", (b["permission"],)).fetchall()
                    self.assertEqual(1, len(rows), b["permission"])
                    if b["permission"] != existing_code:
                        self.assertEqual(b["parentPath"], db.execute("SELECT path FROM sys_menu WHERE id=?", (rows[0][1],)).fetchone()[0])
                    links = set(db.execute("SELECT a.method,a.path FROM sys_menu_api ma JOIN sys_api a ON a.id=ma.api_id WHERE ma.menu_id=? AND a.deleted_at IS NULL", (rows[0][0],)))
                    expected = {(a["method"], a["path"]) for a in b["apis"]}
                    self.assertTrue(expected <= links, b["permission"])
                for page in self.catalog.get("dormantPages", []):
                    self.assertEqual((1, 1), db.execute("SELECT disable,hide FROM sys_menu WHERE path=?", (page["path"],)).fetchone())
                db.close()

    def test_missing_optional_page_does_not_create_orphan_buttons(self):
        db, _, _ = self.database()
        db.execute("DELETE FROM sys_menu WHERE path='/home'")
        sql = (DATABASE / "gb28181/migrations" / f"{VERSION}.sql").read_text()
        db.executescript(sql)
        self.assertEqual(0, db.execute("SELECT COUNT(*) FROM sys_menu WHERE permission LIKE 'gb28181:home:%'").fetchone()[0])
        self.assertEqual(0, db.execute("SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/home/%' AND deleted_at IS NULL").fetchone()[0])
        db.close()

    def test_fresh_baselines_match_migrations(self):
        for suffix, name in [("", "uvp-gb28181.sql"), ("-postgresql", "postgresql_converted.sql"), ("-sqlserver", "sqlserver_converted.sql")]:
            migration = (DATABASE / "gb28181/migrations" / f"{VERSION}{suffix}.sql").read_text().strip()
            self.assertIn(migration, (DATABASE / name).read_text(), name)


if __name__ == "__main__":
    unittest.main()
