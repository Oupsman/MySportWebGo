package models

import (
	"database/sql"
	"gorm.io/driver/postgres"
	_ "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"strings"
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

func CreateOrMigrate(db *DB) error {
	err := db.AutoMigrate(&Users{}, &Equipments{}, &Activity{}, &Route{}, &HealthData{}, &Validations{}, &Keys{}, &Notifications{}, &DeviceTokens{})
	if err != nil {
		return err
	}
	// AutoMigrate doesn't change the type of an existing column: the target weight was whole kilos
	return alterIntegerColumn(db, &Users{}, "WeightObjective", "weight_objective")
}

// alterIntegerColumn gives a column its type in the model when the database still has it as an integer
func alterIntegerColumn(db *DB, model interface{}, field, column string) error {
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	for _, c := range columns {
		if c.Name() != column {
			continue
		}
		switch strings.ToLower(c.DatabaseTypeName()) {
		case "int2", "int4", "int8", "smallint", "integer", "bigint":
			return db.Migrator().AlterColumn(model, field)
		}
	}
	return nil
}
