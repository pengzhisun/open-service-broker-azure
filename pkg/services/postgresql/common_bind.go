package postgresql

import (
	"fmt"
	"net/url"

	"github.com/Azure/open-service-broker-azure/pkg/generate"
	"github.com/Azure/open-service-broker-azure/pkg/service"

	log "github.com/Sirupsen/logrus"
)

func createBinding(
	enforceSSL bool,
	serverName string,
	administratorLoginPassword string,
	fullyQualifiedDomainName string,
	databaseName string,
) (*bindingDetails, error) {
	roleName := generate.NewIdentifier()
	password := generate.NewPassword()

	db, err := getDBConnection(
		enforceSSL,
		serverName,
		administratorLoginPassword,
		fullyQualifiedDomainName,
		primaryDB,
	)
	if err != nil {
		return nil, err
	}
	defer db.Close() // nolint: errcheck

	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("error starting transaction: %s", err)
	}
	defer func() {
		if err != nil {
			if err = tx.Rollback(); err != nil {
				log.WithField("error", err).Error("error rolling back transaction")
			}
		}
	}()
	if _, err = tx.Exec(
		fmt.Sprintf("create role %s with password '%s' login", roleName, password),
	); err != nil {
		return nil, fmt.Errorf(
			`error creating role "%s": %s`,
			roleName,
			err,
		)
	}
	if _, err = tx.Exec(
		fmt.Sprintf("grant %s to %s", databaseName, roleName),
	); err != nil {
		return nil, fmt.Errorf(
			`error adding role "%s" to role "%s": %s`,
			databaseName,
			roleName,
			err,
		)
	}
	if _, err = tx.Exec(
		fmt.Sprintf("alter role %s set role %s", roleName, databaseName),
	); err != nil {
		return nil, fmt.Errorf(
			`error making "%s" the default role for "%s" sessions: %s`,
			databaseName,
			roleName,
			err,
		)
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("error committing transaction: %s", err)
	}
	bd := &bindingDetails{
		LoginName: roleName,
		Password:  service.SecureString(password),
	}
	return bd, err
}

// Create a credential to be returned for binding purposes. This includes a CF
// compatible uri string and a flag to indicate if this connection should
// use ssl. URI is built with the username passed to url.QueryEscape to escape
// the @ in the username
func createCredential(
	fqdn string,
	sslRequired bool,
	serverName string,
	databaseName string,
	bindDetails *bindingDetails,
) credentials {
	username := fmt.Sprintf("%s@%s", bindDetails.LoginName, serverName)
	port := 5432
	var connectionTemplate string
	if sslRequired {
		connectionTemplate = "postgresql://%s:%s@%s:%d/%s?&sslmode=require"

	} else {
		connectionTemplate = "postgresql://%s:%s@%s:%d/%s"
	}
	connectionString := fmt.Sprintf(
		connectionTemplate,
		url.QueryEscape(username),
		url.QueryEscape(string(bindDetails.Password)),
		fqdn,
		port,
		databaseName,
	)

	var jdbcTemplate string
	if sslRequired {
		jdbcTemplate =
			"jdbc:postgresql://%s:%d/%s?user=%s&password=%s&sslmode=require"
	} else {
		jdbcTemplate = "jdbc:postgresql://%s:%d/%s?user=%s&password=%s"
	}

	jdbc := fmt.Sprintf(
		jdbcTemplate,
		fqdn,
		port,
		databaseName,
		url.QueryEscape(username),
		url.QueryEscape(string(bindDetails.Password)),
	)

	return credentials{
		Host:        fqdn,
		Port:        port,
		Database:    databaseName,
		Username:    username,
		Password:    string(bindDetails.Password),
		SSLRequired: sslRequired,
		URI:         connectionString,
		JDBC:        jdbc,
		Tags:        []string{"postgresql"},
	}
}
