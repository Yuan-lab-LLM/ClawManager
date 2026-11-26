package db

import (
	"clawreef/internal/config"
	"clawreef/internal/mysqltls"
	"crypto/tls"
	mysql "github.com/go-sql-driver/mysql"
)

var errBusinessMySQLTLS = mysqltls.ErrConfiguration

func businessTLSMaterial(c config.DatabaseConfig) mysqltls.Material {
	return mysqltls.Material{CAFile: c.TLSCAFile, CASHA256: c.TLSCASHA256, ServerName: c.TLSServerName}
}
func businessMySQLTLSSelected(c config.DatabaseConfig) bool { return businessTLSMaterial(c).Selected() }
func businessMySQLTLS(c config.DatabaseConfig) (*tls.Config, error) {
	return businessTLSMaterial(c).TLSConfig()
}
func businessMySQLDriverConfig(c config.DatabaseConfig, dsn string) (*mysql.Config, error) {
	driver, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, errBusinessMySQLTLS
	}
	if err = businessTLSMaterial(c).Apply(driver); err != nil {
		return nil, err
	}
	return driver, nil
}
