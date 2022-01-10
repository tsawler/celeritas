package main

import (
	"github.com/fatih/color"
)

func doAuth() error {
	checkForDB()

	// migrations
	dbType := cel.DB.DataType

	tx, err := cel.PopConnect()
	if err != nil {
		exitGracefully(err)
	}
	defer tx.Close()

	upBytes, err := templateFS.ReadFile("templates/migrations/auth_tables." + dbType + ".sql")
	if err != nil {
		exitGracefully(err)
	}

	downBytes := []byte("drop table if exists users cascade; drop table if exists tokens cascade; drop table if exists remember_tokens;")
	if err != nil {
		exitGracefully(err)
	}

	err = cel.CreatePopMigration(upBytes, downBytes, "auth", "sql")
	if err != nil {
		exitGracefully(err)
	}

	// run migrations
	err = cel.RunPopMigrations(tx)
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/data/user.go.txt", cel.RootPath + "/data/user.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/data/token.go.txt", cel.RootPath + "/data/token.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/data/remember_token.go.txt", cel.RootPath + "/data/remember_token.go")
	if err != nil {
		exitGracefully(err)
	}

	// copy over middleware
	err = copyFilefromTemplate("templates/middleware/auth.go.txt", cel.RootPath + "/middleware/auth.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/middleware/auth-token.go.txt", cel.RootPath + "/middleware/auth-token.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/middleware/remember.go.txt", cel.RootPath + "/middleware/remember.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/handlers/auth-handlers.go.txt", cel.RootPath + "/handlers/auth-handlers.go")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/mailer/password-reset.html.tmpl", cel.RootPath + "/mail/password-reset.html.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/mailer/password-reset.plain.tmpl", cel.RootPath + "/mail/password-reset.plain.tmpl")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/views/login.jet", cel.RootPath + "/views/login.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/views/forgot.jet", cel.RootPath + "/views/forgot.jet")
	if err != nil {
		exitGracefully(err)
	}

	err = copyFilefromTemplate("templates/views/reset-password.jet", cel.RootPath + "/views/reset-password.jet")
	if err != nil {
		exitGracefully(err)
	}


	color.Yellow("  - users, tokens, and remember_tokens migrations created and executed")
	color.Yellow("  - user and token models created")
	color.Yellow("  - auth middleware created")
	color.Yellow("")
	color.Yellow("Don't forget to add user and token models in data/models.go, and to add appropriate middleware to your routes!")

	return nil
}