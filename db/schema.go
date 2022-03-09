package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	goose "github.com/Financial-Times/cm-goose" // forked from "github.com/pressly/goose"
	log "github.com/sirupsen/logrus"
)

type migration struct {
	cardinal int64
	name     string
	apply    string
	rollback string
}

const (
	dbLockName = "goose"

	ErrDbLockFailure        = "unable to obtain database lock"
	ErrDbReleaseLockFailure = "unable to release database lock"
)

var (
	migrations = []migration{
		{1, "initial-annotations-tables",
			`create table draft_annotations (
			uuid varchar(36) primary key,
			last_modified varchar(32) not null,
			publish_ref varchar(50) not null,
			body mediumtext not null
		);

		create table published_annotations (
			uuid varchar(36) primary key,
			last_modified varchar(32) not null,
			publish_ref varchar(50) not null,
			body mediumtext not null
		);
		`,
			`drop table published_annotations;

		drop table draft_annotations;
		`,
		},
		{2, "add-hash-annotations-tables",
			`alter table draft_annotations add column hash varchar(56) not null;

			alter table published_annotations add column hash varchar(56) not null;
		`,
			`alter table draft_annotations drop column hash;

			alter table published_annotations drop column hash;
		`,
		},
		{3, "initial-draft-content-table",
			`create table draft_content (
			uuid varchar(36) primary key,
			last_modified varchar(32) not null,
			draft_ref varchar(50) not null,
			origin_system varchar(50) not null,
			hash varchar(56) not null,
			body mediumtext not null
		);
		`,
			`drop table draft_content;
		`,
		},
		{4, "add-content-type-draft-content-table",
			`alter table draft_content add column content_type varchar(128) not null;
		`,
			`alter table draft_content drop column content_type;
		`,
		},
	}
	requiredVersion int64
)

func init() {
	goose.SetDialect("mysql")

	for _, step := range migrations {
		goose.AddNamedMigration(step.filename(), exec(step.apply), exec(step.rollback))
		requiredVersion = step.cardinal
	}
}

func (m *migration) filename() string {
	return fmt.Sprintf("%05d_%s.go", m.cardinal, m.name)
}

func (service *AuroraRWService) migrate(apply bool) error {
	currentVersion, err := goose.GetDBVersion(service.conn)
	if err != nil {
		log.WithError(err).Error("unable to discover DB version")
		return err
	}

	if requiredVersion > currentVersion {
		if apply {
			log.WithFields(log.Fields{"from": currentVersion, "to": requiredVersion}).Info("migrating database")
			err = doMigrate(service.conn)
			if err != nil {
				log.WithError(err).Errorf("migrating database from %v to %v failed", currentVersion, requiredVersion)
				err = errors.New(fmt.Sprintf("migrating database from %v to %v failed", currentVersion, requiredVersion))
			}
		} else {
			return errors.New(fmt.Sprintf("migrating database from %v to %v is required", currentVersion, requiredVersion))
		}
	} else if requiredVersion < currentVersion {
		return errors.New(fmt.Sprintf("migrating database DOWN from %v to %v is required", currentVersion, requiredVersion))
	}

	if err == nil {
		service.schemaVersion, _ = goose.GetDBVersion(service.conn)
		log.WithField("schemaVersion", service.schemaVersion).Info("database schema checked")
	}

	return err
}

func doMigrate(conn *sql.DB) error {
	var locked int
	lock, err := conn.Query("SELECT get_lock(?, 1)", dbLockName)
	if err != nil {
		log.WithError(err).Info("unable to obtain database lock")
		return err
	}

	defer lock.Close()
	lock.Next()
	lock.Scan(&locked)
	if locked != 1 {
		log.Warn(ErrDbLockFailure)
		return errors.New(ErrDbLockFailure)
	}

	defer releaseLock(conn)

	return goose.UpTo(conn, ".", requiredVersion)
}

func releaseLock(conn *sql.DB) {
	unlock, err := conn.Query("SELECT release_lock(?)", dbLockName)
	if err != nil {
		log.WithError(err).Error(ErrDbReleaseLockFailure)
	}

	defer unlock.Close()
	unlock.Next()
	var unlocked int
	unlock.Scan(&unlocked)
	unlock.Close()
	if unlocked != 1 {
		log.Error(ErrDbReleaseLockFailure)
	}
}

func exec(sqlStatements string) func(*sql.Tx) error {
	return func(tx *sql.Tx) error {
		for _, stmt := range strings.Split(sqlStatements, ";") {
			if len(strings.TrimSpace(stmt)) == 0 {
				continue
			}

			log.Infof("apply: %s", stmt)
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}

		return nil
	}
}
