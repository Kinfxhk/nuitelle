package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// peopleSchema is applied on every startup; it is idempotent like the other
// schemas, so an existing database picks the tables up automatically.
const peopleSchema = `
CREATE TABLE IF NOT EXISTS people(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  aliases TEXT NOT NULL DEFAULT '',
  image TEXT NOT NULL DEFAULT '',
  thumb TEXT NOT NULL DEFAULT '',
  height_cm INTEGER NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS people_name ON people(lower(name));
CREATE TABLE IF NOT EXISTS media_people(
  media_id TEXT NOT NULL,
  person_id TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'actor',
  PRIMARY KEY(media_id, person_id)
);
CREATE INDEX IF NOT EXISTS media_people_person ON media_people(person_id);
`

const (
	maxPersonName    = 120
	maxPersonAliases = 400
	maxPersonNote    = 2000
	maxPersonHeight  = 300
)

// ErrPersonExists is returned when a name already resolves to a person. The
// returned Person is the existing row, so callers can link instead of create.
var ErrPersonExists = errors.New("person already exists")

// Person is one performer. Image and Thumb are file names inside the people
// directory, never absolute paths.
type Person struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Aliases   string `json:"aliases"`
	Image     string `json:"image"`
	Thumb     string `json:"thumb"`
	HeightCM  int    `json:"height_cm"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"created_at"`
	Count     int64  `json:"count"`
}

// PersonPatch is a partial update; nil fields are left untouched.
type PersonPatch struct {
	Name     *string `json:"name"`
	Aliases  *string `json:"aliases"`
	HeightCM *int    `json:"height_cm"`
	Note     *string `json:"note"`
}

// PersonInput is the create payload.
type PersonInput struct {
	Name     string `json:"name"`
	Aliases  string `json:"aliases"`
	HeightCM int    `json:"height_cm"`
	Note     string `json:"note"`
}

type rowScanner interface {
	Scan(dest ...any) error
}

type dbConn interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func scanPerson(sc rowScanner) (Person, error) {
	var p Person
	err := sc.Scan(&p.ID, &p.Name, &p.Aliases, &p.Image, &p.Thumb, &p.HeightCM, &p.Note, &p.CreatedAt, &p.Count)
	return p, err
}

const personColumns = `p.id,p.name,p.aliases,p.image,p.thumb,p.height_cm,p.note,p.created_at,
 COALESCE((SELECT COUNT(*) FROM media_people mp JOIN media m ON m.id=mp.media_id WHERE mp.person_id=p.id AND m.trashed=0),0) AS cnt`

func cleanPersonName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

// personLimit normalises list pagination; 60 per page is the documented default.
func personLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 60
	}
	return limit
}

// ListPeople returns people with a count of their active media items.
func (s *Store) ListPeople(q, sortBy string, limit, offset int) ([]Person, int64, error) {
	limit = personLimit(limit)
	if offset < 0 {
		offset = 0
	}
	where := "1=1"
	args := []any{}
	if q = strings.TrimSpace(q); q != "" {
		where = `(lower(p.name) LIKE ? ESCAPE '\' OR lower(p.aliases) LIKE ? ESCAPE '\')`
		like := "%" + strings.ToLower(escLike(q)) + "%"
		args = append(args, like, like)
	}
	var total int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM people p WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := `p.name COLLATE NOCASE ASC`
	switch sortBy {
	case "count":
		order = `cnt DESC, p.name COLLATE NOCASE ASC`
	case "recent":
		// rowid breaks ties for people created in the same second.
		order = `p.created_at DESC, p.rowid DESC`
	}
	query := `SELECT ` + personColumns + ` FROM people p WHERE ` + where + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// GetPerson loads one person by id.
func (s *Store) GetPerson(id string) (Person, error) {
	return scanPerson(s.db.QueryRow(`SELECT `+personColumns+` FROM people p WHERE p.id=?`, id))
}

// PersonByName resolves a name or alias, case-insensitively.
func (s *Store) PersonByName(name string) (Person, bool) {
	name = cleanPersonName(name)
	if name == "" {
		return Person{}, false
	}
	p, err := scanPerson(s.db.QueryRow(`SELECT `+personColumns+` FROM people p WHERE lower(p.name)=lower(?)`, name))
	if err == nil {
		return p, true
	}
	// Aliases are a comma separated list.
	rows, err := s.db.Query(`SELECT ` + personColumns + ` FROM people p WHERE aliases<>''`)
	if err != nil {
		return Person{}, false
	}
	defer rows.Close()
	for rows.Next() {
		cand, err := scanPerson(rows)
		if err != nil {
			continue
		}
		for _, a := range splitAliases(cand.Aliases) {
			if strings.EqualFold(a, name) {
				return cand, true
			}
		}
	}
	return Person{}, false
}

func splitAliases(v string) []string {
	fields := strings.Split(v, ",")
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// NormalizeAliases rewrites an alias list into "a, b" form.
func normalizeAliases(v string) string {
	list := splitAliases(v)
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for _, a := range list {
		k := strings.ToLower(a)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, a)
	}
	return strings.Join(out, ", ")
}

func validPersonFields(in PersonInput) (PersonInput, error) {
	in.Name = cleanPersonName(in.Name)
	if in.Name == "" {
		return in, fmt.Errorf("name required")
	}
	if len([]rune(in.Name)) > maxPersonName {
		return in, fmt.Errorf("name too long")
	}
	in.Aliases = normalizeAliases(in.Aliases)
	if len([]rune(in.Aliases)) > maxPersonAliases {
		return in, fmt.Errorf("aliases too long")
	}
	in.Note = strings.TrimSpace(in.Note)
	if len([]rune(in.Note)) > maxPersonNote {
		return in, fmt.Errorf("note too long")
	}
	if in.HeightCM < 0 || in.HeightCM > maxPersonHeight {
		return in, fmt.Errorf("height_cm out of range")
	}
	return in, nil
}

// CreatePerson inserts a person. A case-insensitive duplicate returns
// ErrPersonExists together with the existing row.
func (s *Store) CreatePerson(in PersonInput) (Person, error) {
	in, err := validPersonFields(in)
	if err != nil {
		return Person{}, err
	}
	if existing, ok := s.PersonByName(in.Name); ok {
		return existing, ErrPersonExists
	}
	p := Person{
		ID:        newPersonID(),
		Name:      in.Name,
		Aliases:   in.Aliases,
		HeightCM:  in.HeightCM,
		Note:      in.Note,
		CreatedAt: time.Now().Unix(),
	}
	s.mu.Lock()
	_, err = s.db.Exec(`INSERT INTO people(id,name,aliases,image,thumb,height_cm,note,created_at) VALUES(?,?,?,'','',?,?,?)`,
		p.ID, p.Name, p.Aliases, p.HeightCM, p.Note, p.CreatedAt)
	s.mu.Unlock()
	if err != nil {
		// Lost a race with a concurrent create; resolve to the winner.
		if existing, ok := s.PersonByName(in.Name); ok {
			return existing, ErrPersonExists
		}
		return Person{}, err
	}
	return p, nil
}

// createPersonRow inserts without the duplicate pre-check; used when a media
// item references a name that does not exist yet.
func createPersonRow(db dbConn, in PersonInput) (Person, error) {
	in, err := validPersonFields(in)
	if err != nil {
		return Person{}, err
	}
	p := Person{ID: newPersonID(), Name: in.Name, Aliases: in.Aliases, HeightCM: in.HeightCM, Note: in.Note, CreatedAt: time.Now().Unix()}
	_, err = db.Exec(`INSERT INTO people(id,name,aliases,image,thumb,height_cm,note,created_at) VALUES(?,?,?,'','',?,?,?)`,
		p.ID, p.Name, p.Aliases, p.HeightCM, p.Note, p.CreatedAt)
	return p, err
}

func newPersonID() string {
	return fmt.Sprintf("p%d", time.Now().UnixNano())
}

// UpdatePerson applies a partial update and returns the fresh row.
func (s *Store) UpdatePerson(id string, patch PersonPatch) (Person, error) {
	cur, err := s.GetPerson(id)
	if err != nil {
		return Person{}, err
	}
	in := PersonInput{Name: cur.Name, Aliases: cur.Aliases, HeightCM: cur.HeightCM, Note: cur.Note}
	if patch.Name != nil {
		in.Name = *patch.Name
	}
	if patch.Aliases != nil {
		in.Aliases = *patch.Aliases
	}
	if patch.HeightCM != nil {
		in.HeightCM = *patch.HeightCM
	}
	if patch.Note != nil {
		in.Note = *patch.Note
	}
	in, err = validPersonFields(in)
	if err != nil {
		return Person{}, err
	}
	if !strings.EqualFold(in.Name, cur.Name) {
		if other, ok := s.PersonByName(in.Name); ok && other.ID != id {
			return other, ErrPersonExists
		}
	}
	s.mu.Lock()
	_, err = s.db.Exec(`UPDATE people SET name=?,aliases=?,height_cm=?,note=? WHERE id=?`,
		in.Name, in.Aliases, in.HeightCM, in.Note, id)
	s.mu.Unlock()
	if err != nil {
		return Person{}, err
	}
	return s.GetPerson(id)
}

// DeletePerson removes the person and every link; media and tags stay intact.
func (s *Store) DeletePerson(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM media_people WHERE person_id=?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM people WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// SetPersonImage records the generated file names for a person.
func (s *Store) SetPersonImage(id, image, thumb string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`UPDATE people SET image=?,thumb=? WHERE id=?`, image, thumb, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// LinkPerson attaches one person to one media item. Unknown pairs are ignored.
func (s *Store) LinkPerson(mediaID, personID, role string) error {
	role = strings.TrimSpace(role)
	if role == "" {
		role = "actor"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT OR IGNORE INTO media_people(media_id,person_id,role) VALUES(?,?,?)`, mediaID, personID, role)
	return err
}

func (s *Store) UnlinkPerson(mediaID, personID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM media_people WHERE media_id=? AND person_id=?`, mediaID, personID)
	return err
}

// PeopleForMedia lists the people on one item, alphabetically.
func (s *Store) PeopleForMedia(mediaID string) ([]Person, error) {
	rows, err := s.db.Query(`SELECT `+personColumns+` FROM people p
 JOIN media_people mp ON mp.person_id=p.id WHERE mp.media_id=? ORDER BY p.name COLLATE NOCASE`, mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AssignPeople adds and removes people by name on one media item. Names that do
// not exist yet are created on the fly, matching the tag UX.
func (s *Store) AssignPeople(mediaID string, add, remove []string) ([]Person, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, raw := range add {
		name := cleanPersonName(raw)
		if name == "" {
			continue
		}
		pid, err := personIDByName(tx, name)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			p, cerr := createPersonRow(tx, PersonInput{Name: name})
			if cerr != nil {
				return nil, cerr
			}
			pid = p.ID
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO media_people(media_id,person_id,role) VALUES(?,?,'actor')`, mediaID, pid); err != nil {
			return nil, err
		}
	}
	for _, raw := range remove {
		name := cleanPersonName(raw)
		if name == "" {
			continue
		}
		pid, err := personIDByName(tx, name)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`DELETE FROM media_people WHERE media_id=? AND person_id=?`, mediaID, pid); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.PeopleForMedia(mediaID)
}

// personIDByName resolves a name or alias inside a transaction.
func personIDByName(db dbConn, name string) (string, error) {
	var id string
	err := db.QueryRow(`SELECT id FROM people WHERE lower(name)=lower(?)`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	rows, err := db.Query(`SELECT id,aliases FROM people WHERE aliases<>''`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var pid, aliases string
		if rows.Scan(&pid, &aliases) != nil {
			continue
		}
		for _, a := range splitAliases(aliases) {
			if strings.EqualFold(a, name) {
				return pid, nil
			}
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", sql.ErrNoRows
}

// personIDs normalises a "id,id" filter value.
func personIDs(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
