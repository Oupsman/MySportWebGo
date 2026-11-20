package models

import (
	"context"
	"database/sql"
	"gorm.io/driver/postgres"
	_ "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"time"
)

type DB struct {
	gorm.DB
}

func ConnectToDB(driver string, dsn string) (*DB, error) {

	var err error
	var sqlDB *sql.DB
	var db *gorm.DB

	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})

	if err != nil {
		return nil, err
	}

	sqlDB, err = db.DB()

	if err != nil {
		return nil, err
	}

	// SetMaxIdleConns sets the maximum number of connections in the idle connection pool.
	sqlDB.SetMaxIdleConns(5)

	// SetMaxOpenConns sets the maximum number of open connections to the database.
	sqlDB.SetMaxOpenConns(100)

	// SetConnMaxLifetime sets the maximum amount of time a connection may be reused.
	sqlDB.SetConnMaxLifetime(time.Minute * 5)

	return &DB{*db}, nil
}

func (db *DB) Ping() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

// Counts returns the number of users and of activities, for the metrics
func (db *DB) Counts(ctx context.Context) (users, activities int64, err error) {
	tx := db.WithContext(ctx)
	if err := tx.Model(&Users{}).Count(&users).Error; err != nil {
		return 0, 0, err
	}
	err = tx.Model(&Activity{}).Count(&activities).Error
	return users, activities, err
}
