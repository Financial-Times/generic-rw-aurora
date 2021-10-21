package db

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/Financial-Times/generic-rw-aurora/config"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type ServiceSchemaTestSuite struct {
	suite.Suite
	dbAdminUrl  string
	dbAdminConn *sql.DB
	dbUrl       string
	dbConn      *sql.DB
}

const (
	errVersionMismatch = "Database schema is mismatched to this service"
)

func TestServiceSchemaTestSuite(t *testing.T) {
	testSuite := ServiceSchemaTestSuite{}
	testSuite.dbAdminUrl = getDatabaseURL(t)
	suite.Run(t, &testSuite)
}

func (s *ServiceSchemaTestSuite) SetupTest() {
	conn, err := sql.Open("mysql", s.dbAdminUrl)
	require.NoError(s.T(), err)
	s.dbAdminConn = conn

	pacSchema := "pac_test"
	pacUser := "pac_test_user"

	err = cleanDatabase(s.dbAdminConn, pacUser, pacSchema)
	require.NoError(s.T(), err)

	pacPassword := uuid.New().String()
	err = createDatabase(s.dbAdminConn, pacUser, pacPassword, pacSchema)
	require.NoError(s.T(), err)

	i := strings.Index(s.dbAdminUrl, "@")
	j := strings.Index(s.dbAdminUrl, "/")
	s.dbUrl = fmt.Sprintf("%s:%s@%s/%s", pacUser, pacPassword, s.dbAdminUrl[i+1:j], pacSchema)

	conn, err = Connect(s.dbUrl, 5)
	require.NoError(s.T(), err)
	s.dbConn = conn
}

func (s *ServiceSchemaTestSuite) TearDownTest() {
	if s.dbAdminConn != nil {
		defer s.dbAdminConn.Close()
	}

	if s.dbConn != nil {
		defer s.dbConn.Close()
	}
}

func cleanDatabase(conn *sql.DB, user string, schema string) error {
	userExists, err := checkUserExists(conn, user)
	if err != nil {
		return err
	}

	schemaExists, err := checkSchemaExists(conn, schema)
	if err != nil {
		return err
	}

	if userExists {
		log.Infof("dropping user %s", user)
		_, err = conn.Exec(fmt.Sprintf(`DROP USER %s`, user))
		if err != nil {
			return err
		}
	}

	if schemaExists {
		log.Infof("dropping schema %s", schema)
		_, err = conn.Exec(fmt.Sprintf(`DROP DATABASE %s`, schema))
	}

	return err
}

func createDatabase(conn *sql.DB, user string, password string, schema string) error {
	_, err := conn.Exec(fmt.Sprintf(`CREATE DATABASE %s`, schema))
	if err != nil {
		return err
	}

	_, err = conn.Exec(fmt.Sprintf(`CREATE USER %s IDENTIFIED BY '%s'`, user, password))
	if err != nil {
		return err
	}

	_, err = conn.Exec(fmt.Sprintf(`GRANT ALL ON %s.* TO %s`, schema, user))
	return err
}

func checkUserExists(conn *sql.DB, user string) (bool, error) {
	var count int
	rows, err := conn.Query("SELECT COUNT(*) FROM information_schema.user_privileges WHERE grantee LIKE ?", fmt.Sprintf(`'%s'%%`, user))
	if err != nil {
		return false, err
	}

	defer rows.Close()
	rows.Next()
	rows.Scan(&count)

	return count > 0, nil
}

func checkSchemaExists(conn *sql.DB, schema string) (bool, error) {
	var count int
	rows, err := conn.Query("SELECT COUNT(*) FROM information_schema.schema_privileges WHERE table_schema = ?", schema)
	if err != nil {
		return false, err
	}

	defer rows.Close()
	rows.Next()
	rows.Scan(&count)

	return count > 0, nil
}

func (s *ServiceSchemaTestSuite) TestPing() {
	srv := NewService(s.dbConn, false, &config.Config{})

	msg, err := srv.Ping()
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), "Ping OK", msg)
}

func (s *ServiceSchemaTestSuite) TestSchemaCheckWithoutMigrating() {
	srv := NewService(s.dbConn, false, &config.Config{})

	msg, err := srv.SchemaCheck()
	assert.EqualError(s.T(), err, fmt.Sprintf("migrating database from 0 to %d is required", requiredVersion))
	assert.Equal(s.T(), errVersionMismatch, msg)
}

func (s *ServiceSchemaTestSuite) TestSchemaCheckMigrateWhilstLocked() {
	var locked int
	lock, err := s.dbAdminConn.Query("SELECT get_lock(?, 1)", dbLockName)
	require.NoError(s.T(), err, "admin connection was unable to obtain a lock")
	defer lock.Close()
	lock.Next()
	lock.Scan(&locked)
	//lock.Close()
	require.Equal(s.T(), 1, locked, "admin connection was unable to obtain a lock")
	defer s.dbAdminConn.Exec("SELECT release_lock(?)", dbLockName)

	// try to migrate but another connection has an exclusive lock
	srv := NewService(s.dbConn, true, &config.Config{})

	msg, err := srv.SchemaCheck()
	assert.EqualError(s.T(), err, fmt.Sprintf("migrating database from 0 to %d failed", requiredVersion))
	assert.Equal(s.T(), errVersionMismatch, msg)
}

func (s *ServiceSchemaTestSuite) TestSchemaMigrate() {
	srv := NewService(s.dbConn, true, &config.Config{})

	msg, err := srv.SchemaCheck()
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), fmt.Sprintf("Database schema is at version %d", requiredVersion), msg)
}
