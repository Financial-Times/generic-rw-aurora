package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Financial-Times/generic-rw-aurora/config"
	tid "github.com/Financial-Times/transactionid-utils-go"
	"github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
	logTest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	testTable                      = "published_annotations"
	testTableWithConflictDetection = "draft_annotations"
	testTableWithMetadata          = "draft_content"
	testKeyColumn                  = "uuid"
	testDocColumn                  = "body"
	timestampMetadata = "_timestamp"
	lastModifiedColumn             = "last_modified"
	publishRefColumn               = "publish_ref"
	testDocTemplate                = `{"foo":"%s"}`
)

type ServiceRWTestSuite struct {
	suite.Suite
	dbAdminUrl string
	dbConn     *sql.DB
	service    *AuroraRWService
}

func TestServiceRWTestSuite(t *testing.T) {
	testSuite := ServiceRWTestSuite{}
	testSuite.dbAdminUrl = getDatabaseURL(t)

	suite.Run(t, &testSuite)
}

func (s *ServiceRWTestSuite) SetupSuite() {
	conn, err := sql.Open("mysql", s.dbAdminUrl)
	require.NoError(s.T(), err)
	defer conn.Close()

	pacSchema := "pac_test"
	pacUser := "pac_test_user"

	err = cleanDatabase(conn, pacUser, pacSchema)
	require.NoError(s.T(), err)

	pacPassword := uuid.NewV4().String()
	err = createDatabase(conn, pacUser, pacPassword, pacSchema)
	require.NoError(s.T(), err)

	i := strings.Index(s.dbAdminUrl, "@")
	j := strings.Index(s.dbAdminUrl, "/")
	dbUrl := fmt.Sprintf("%s:%s@%s/%s", pacUser, pacPassword, s.dbAdminUrl[i+1:j], pacSchema)

	conn, err = Connect(dbUrl)
	require.NoError(s.T(), err)

	cfg, err := config.ReadConfig("../config.yml")
	require.NoError(s.T(), err)

	s.dbConn = conn
	s.service = NewService(conn, true, cfg)
}

func (s *ServiceRWTestSuite) TearDownSuite() {
	s.dbConn.Close()
}

func (s *ServiceRWTestSuite) TestRead() {
	testKey := uuid.NewV4().String()

	testTID := "tid_testread"

	testDocBody := fmt.Sprintf(testDocTemplate, time.Now().String())
	testDoc := NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID)

	testCtx := tid.TransactionAwareContext(context.Background(), testTID)

	params := map[string]string{"id": testKey}

	status, expectedDocHash, err := s.service.Write(context.Background(), testTable, testKey, testDoc, params, "")
	require.NoError(s.T(), err)
	require.Equal(s.T(), Created, status)

	actual, err := s.service.Read(testCtx, testTable, testKey)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), testDoc.Body, actual.Body, "document read from store")
	assert.Equal(s.T(), expectedDocHash, actual.Hash)
}

func (s *ServiceRWTestSuite) TestReadNotFound() {
	testKey := uuid.NewV4().String()
	testTID := "tid_testread"
	testCtx := tid.TransactionAwareContext(context.Background(), testTID)
	_, err := s.service.Read(testCtx, testTable, testKey)
	assert.EqualError(s.T(), err, sql.ErrNoRows.Error())
}

func (s *ServiceRWTestSuite) TestReadWithMetadata() {
	testKey := uuid.NewV4().String()

	testTID := "tid_testread"
	testSystem := "foo-bar-baz"
	testHeader := "X-Origin-System-Id"

	foo := time.Now().String()

	testDocBody := fmt.Sprintf(testDocTemplate, foo)
	testDoc := NewDocument([]byte(testDocBody))
	lastModified := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	testDoc.Metadata.Set(timestampMetadata, lastModified)
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID)
	testDoc.Metadata.Set(strings.ToLower(testHeader), testSystem)

	testCtx := tid.TransactionAwareContext(context.Background(), testTID)

	params := map[string]string{"id": testKey}

	status, expectedDocHash, err := s.service.Write(context.Background(), testTableWithMetadata, testKey, testDoc, params, "")
	require.NoError(s.T(), err)
	require.Equal(s.T(), Created, status)

	actual, err := s.service.Read(testCtx, testTableWithMetadata, testKey)
	assert.NoError(s.T(), err)
	actualDoc := make(map[string]string)
	err = json.Unmarshal(actual.Body, &actualDoc)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), foo, actualDoc["foo"], "document read from store")
	assert.Equal(s.T(), lastModified, actualDoc["lastModified"], "last modified")
	assert.Equal(s.T(), testTID, actualDoc["draftReference"], "draftReference")
	assert.Equal(s.T(), expectedDocHash, actual.Hash)
	assert.Equal(s.T(), testSystem, actual.Metadata[testHeader])
}

func (s *ServiceRWTestSuite) TestWriteCreateWithoutConflictDetection() {
	testKey := uuid.NewV4().String()
	testLastModified := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	testTID := "tid_testcreate"

	testDocBody := fmt.Sprintf(testDocTemplate, time.Now().String())
	testDoc := NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, testLastModified)
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID)

	params := map[string]string{"id": testKey}

	testCtx := tid.TransactionAwareContext(context.Background(), testTID)

	status, docHash, err := s.service.Write(testCtx, testTable, testKey, testDoc, params, "")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), Created, status)

	expectedValuePerCol := map[string]string{
		testDocColumn:      testDocBody,
		lastModifiedColumn: testLastModified,
		publishRefColumn:   testTID,
		hashColumn:         docHash,
	}

	s.assertExpectedDataInDB(testKey, testKeyColumn, testTable, expectedValuePerCol)
}

func (s *ServiceRWTestSuite) TestWriteUpdateWithoutConflictDetection() {
	testKey := uuid.NewV4().String()
	testCreateLastModified := time.Now().Truncate(time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	testDocBody := fmt.Sprintf(testDocTemplate, testCreateLastModified)
	testCreatePublishRef := "tid_testupdate_1"

	testCtx := tid.TransactionAwareContext(context.Background(), testCreatePublishRef)

	testDoc := NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, testCreateLastModified)
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testCreatePublishRef)

	params := map[string]string{"id": testKey}

	_, _, err := s.service.Write(testCtx, testTable, testKey, testDoc, params, "")
	require.NoError(s.T(), err)

	testUpdateLastModified := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	testDocBody = fmt.Sprintf(testDocTemplate, testUpdateLastModified)
	testUpdatePublishRef := "tid_testupdate_2"

	testDoc = NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, testUpdateLastModified)
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testUpdatePublishRef)

	testCtx = tid.TransactionAwareContext(context.Background(), testCreatePublishRef)

	status, docHash, err := s.service.Write(testCtx, testTable, testKey, testDoc, params, "")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), Updated, status)

	expectedValuePerCol := map[string]string{
		testDocColumn:      testDocBody,
		lastModifiedColumn: testUpdateLastModified,
		publishRefColumn:   testUpdatePublishRef,
		hashColumn:         docHash,
	}

	s.assertExpectedDataInDB(testKey, testKeyColumn, testTable, expectedValuePerCol)
}

func (s *ServiceRWTestSuite) TestWriteCreateWithoutConflict() {
	hook := logTest.NewGlobal()
	testKey := uuid.NewV4().String()
	testLastModified := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	testTID := "tid_testcreate"

	testDocBody := fmt.Sprintf(testDocTemplate, time.Now().String())
	testDoc := NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, testLastModified)
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID)

	params := map[string]string{"id": testKey}

	testCtx := tid.TransactionAwareContext(context.Background(), testTID)

	status, docHash, err := s.service.Write(testCtx, testTableWithConflictDetection, testKey, testDoc, params, "")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), Created, status)

	expectedValuePerCol := map[string]string{
		testDocColumn:      testDocBody,
		lastModifiedColumn: testLastModified,
		publishRefColumn:   testTID,
		hashColumn:         docHash,
	}

	s.assertExpectedDataInDB(testKey, testKeyColumn, testTableWithConflictDetection, expectedValuePerCol)

	assert.Empty(s.T(), hook.AllEntries())
}

func (s *ServiceRWTestSuite) TestWriteCreateWithConflict() {
	hook := logTest.NewGlobal()
	testKey := uuid.NewV4().String()
	testLastModified := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	testTID1 := "tid_testcreate_1"
	testTID2 := "tid_testcreate_2"

	testDocBody := fmt.Sprintf(testDocTemplate, time.Now().String())
	testDoc := NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, testLastModified)
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID1)

	params := map[string]string{"id": testKey}

	testCtx := tid.TransactionAwareContext(context.Background(), testTID1)

	status, docHash, err := s.service.Write(testCtx, testTableWithConflictDetection, testKey, testDoc, params, "")
	require.NoError(s.T(), err)
	require.Equal(s.T(), Created, status)

	testLastModified2 := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	testDocBody = fmt.Sprintf(testDocTemplate, testLastModified)

	testDoc = NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, testLastModified2)
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID2)

	testCtx = tid.TransactionAwareContext(context.Background(), testTID2)

	status, docHash, err = s.service.Write(testCtx, testTableWithConflictDetection, testKey, testDoc, params, "")
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), Updated, status)

	expectedValuePerCol := map[string]string{
		testDocColumn:      testDocBody,
		lastModifiedColumn: testLastModified2,
		publishRefColumn:   testTID2,
		hashColumn:         docHash,
	}

	s.assertExpectedDataInDB(testKey, testKeyColumn, testTableWithConflictDetection, expectedValuePerCol)

	assert.Equal(s.T(), "document hash conflict detected while updating document", hook.LastEntry().Message)
	assert.Equal(s.T(), logrus.WarnLevel, hook.LastEntry().Level)
	assert.Equal(s.T(), testKey, hook.LastEntry().Data["key"])
	assert.Equal(s.T(), testTableWithConflictDetection, hook.LastEntry().Data["table"])
	assert.Equal(s.T(), testTID2, hook.LastEntry().Data[tid.TransactionIDKey])
}

func (s *ServiceRWTestSuite) TestUpdateWithoutConflict() {
	hook := logTest.NewGlobal()
	testKey := uuid.NewV4().String()

	testTID1 := "tid_testupdate_1"
	testTID2 := "tid_testupdate_2"

	testDocBody := fmt.Sprintf(testDocTemplate, time.Now().String())
	testDoc := NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID1)

	params := map[string]string{"id": testKey}

	testCtx := tid.TransactionAwareContext(context.Background(), testTID1)

	status, previousDocHash, err := s.service.Write(testCtx, testTableWithConflictDetection, testKey, testDoc, params, "")
	require.NoError(s.T(), err)
	require.Equal(s.T(), Created, status)

	testLastModified := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	testDocBody = fmt.Sprintf(testDocTemplate, time.Now().String())
	testDoc = NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, testLastModified)
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID2)

	status, docHash, err := s.service.Write(testCtx, testTableWithConflictDetection, testKey, testDoc, params, previousDocHash)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), status, Updated)

	expectedValuePerCol := map[string]string{
		testDocColumn:      testDocBody,
		lastModifiedColumn: testLastModified,
		publishRefColumn:   testTID2,
		hashColumn:         docHash,
	}

	s.assertExpectedDataInDB(testKey, testKeyColumn, testTableWithConflictDetection, expectedValuePerCol)

	assert.Empty(s.T(), hook.AllEntries())
}

func (s *ServiceRWTestSuite) TestUpdateWithConflict() {
	hook := logTest.NewGlobal()
	testKey := uuid.NewV4().String()

	testTID1 := "tid_testupdate_1"
	testTID2 := "tid_testupdate_2"

	testDocBody := fmt.Sprintf(testDocTemplate, time.Now().String())
	testDoc := NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID1)

	params := map[string]string{"id": testKey}

	testCtx := tid.TransactionAwareContext(context.Background(), testTID1)

	status, _, err := s.service.Write(testCtx, testTableWithConflictDetection, testKey, testDoc, params, "")
	require.NoError(s.T(), err)
	require.Equal(s.T(), Created, status)

	testLastModified := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	testDocBody = fmt.Sprintf(testDocTemplate, time.Now().String())
	testDoc = NewDocument([]byte(testDocBody))
	testDoc.Metadata.Set(timestampMetadata, testLastModified)
	testDoc.Metadata.Set(strings.ToLower(tid.TransactionIDHeader), testTID2)

	aVeryOldHash := "01234567890123456789012345678901234567890123456789012345"

	testCtx = tid.TransactionAwareContext(context.Background(), testTID2)

	status, docHash, err := s.service.Write(testCtx, testTableWithConflictDetection, testKey, testDoc, params, aVeryOldHash)
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), status, Updated)

	expectedValuePerCol := map[string]string{
		testDocColumn:      testDocBody,
		lastModifiedColumn: testLastModified,
		publishRefColumn:   testTID2,
		hashColumn:         docHash,
	}

	s.assertExpectedDataInDB(testKey, testKeyColumn, testTableWithConflictDetection, expectedValuePerCol)

	assert.Equal(s.T(), "document hash conflict detected while updating document", hook.LastEntry().Message)
	assert.Equal(s.T(), logrus.WarnLevel, hook.LastEntry().Level)
	assert.Equal(s.T(), testKey, hook.LastEntry().Data["key"])
	assert.Equal(s.T(), testTableWithConflictDetection, hook.LastEntry().Data["table"])
	assert.Equal(s.T(), testTID2, hook.LastEntry().Data[tid.TransactionIDKey])
}

func (s *ServiceRWTestSuite) assertExpectedDataInDB(key string, keyColumn string, table string, expectedValuePerCol map[string]string) {
	var actualValues []interface{}
	var expectedValues []string
	var columns []string
	columnStmt := ""
	for column, expectedValue := range expectedValuePerCol {
		columns = append(columns, column)
		columnStmt += "," + column
		actualValue := new(string)
		actualValues = append(actualValues, actualValue)
		expectedValues = append(expectedValues, expectedValue)
	}
	columnStmt = columnStmt[1:]

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = ?", columnStmt, table, keyColumn)
	row := s.dbConn.QueryRow(query, key)
	err := row.Scan(actualValues...)
	require.NoError(s.T(), err)

	for i, expectedValue := range expectedValues {
		assert.Equal(s.T(), expectedValue, *actualValues[i].(*string), fmt.Sprintf("Value does not match for column %s", columns[i]))
	}
}
