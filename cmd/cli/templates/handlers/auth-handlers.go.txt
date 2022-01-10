package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/github"
	"github.com/markbates/goth/providers/google"
	"log"
	"myapp/data"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/CloudyKit/jet/v6"
	"github.com/tsawler/celeritas/mailer"
	"github.com/tsawler/celeritas/urlsigner"
)

// UserLogin displays the login page
func (h *Handlers) UserLogin(w http.ResponseWriter, r *http.Request) {
	err := h.App.Render.Page(w, r, "login", nil, nil)
	if err != nil {
		h.App.ErrorLog.Println(err)
	}
}

// PostUserLogin attempts to log a user in
func (h *Handlers) PostUserLogin(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		w.Write([]byte(err.Error()))
		return
	}

	email := r.Form.Get("email")
	password := r.Form.Get("password")

	user, err := h.Models.Users.GetByEmail(email)
	if err != nil {
		w.Write([]byte(err.Error()))
		return
	}

	matches, err := user.PasswordMatches(password)
	if err != nil {
		w.Write([]byte("Error validating password"))
		return
	}

	if !matches {
		w.Write([]byte("Invalid password!"))
		return
	}

	// did the user check remember me?
	if r.Form.Get("remember") == "remember" {
		randomString := h.randomString(12)
		hasher := sha256.New()
		_, err := hasher.Write([]byte(randomString))
		if err != nil {
			h.App.ErrorStatus(w, http.StatusBadRequest)
			return
		}

		sha := base64.URLEncoding.EncodeToString(hasher.Sum(nil))
		rm := data.RememberToken{}
		err = rm.InsertToken(user.ID, sha)
		if err != nil {
			h.App.ErrorStatus(w, http.StatusBadRequest)
			return
		}

		// set a cookie
		expire := time.Now().Add(365 * 24 * 60 * 60 * time.Second)
		cookie := http.Cookie{
			Name:     fmt.Sprintf("_%s_remember", h.App.AppName),
			Value:    fmt.Sprintf("%d|%s", user.ID, sha),
			Path:     "/",
			Expires:  expire,
			HttpOnly: true,
			Domain:   h.App.Session.Cookie.Domain,
			MaxAge:   315350000,
			Secure:   h.App.Session.Cookie.Secure,
			SameSite: http.SameSiteStrictMode,
		}
		http.SetCookie(w, &cookie)
		// save hash in session
		h.App.Session.Put(r.Context(), "remember_token", sha)
	}

	h.App.Session.Put(r.Context(), "userID", user.ID)

	http.Redirect(w, r, "/", http.StatusSeeOther)

}

// Logout logs the user out, removes any remember me cookie, and deletes
// remember token from the database, if it exists
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	// delete the remember token if it exists
	if h.App.Session.Exists(r.Context(), "remember_token") {
		rt := data.RememberToken{}
		_ = rt.Delete(h.App.Session.GetString(r.Context(), "remember_token"))
	}

	h.socialLogout(w, r)

	// delete cookie
	newCookie := http.Cookie{
		Name:     fmt.Sprintf("_%s_remember", h.App.AppName),
		Value:    "",
		Path:     "/",
		Expires:  time.Now().Add(-100 * time.Hour),
		HttpOnly: true,
		Domain:   h.App.Session.Cookie.Domain,
		MaxAge:   -1,
		Secure:   h.App.Session.Cookie.Secure,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, &newCookie)

	h.App.Session.RenewToken(r.Context())
	h.App.Session.Remove(r.Context(), "userID")
	h.App.Session.Remove(r.Context(), "remember_token")
	h.App.Session.Destroy(r.Context())
	h.App.Session.RenewToken(r.Context())

	http.Redirect(w, r, "/users/login", http.StatusSeeOther)
}

func (h *Handlers) Forgot(w http.ResponseWriter, r *http.Request) {
	err := h.render(w, r, "forgot", nil, nil)
	if err != nil {
		h.App.ErrorLog.Println("Error rendering: ", err)
		h.App.Error500(w, r)
	}
}

// PostForgot looks up a user by email, and if the user is found, generates
// an email with a singed link to the reset password form
func (h *Handlers) PostForgot(w http.ResponseWriter, r *http.Request) {
	// parse form
	err := r.ParseForm()
	if err != nil {
		h.App.ErrorStatus(w, http.StatusBadRequest)
		return
	}

	// verify that supplied email exists
	var u *data.User
	email := r.Form.Get("email")
	u, err = u.GetByEmail(email)
	if err != nil {
		h.App.ErrorStatus(w, http.StatusBadRequest)
		return
	}

	// create a link to password reset form
	link := fmt.Sprintf("%s/users/reset-password?email=%s", h.App.Server.URL, email)

	// sign the link
	sign := urlsigner.Signer{
		Secret: []byte(h.App.EncryptionKey),
	}

	signedLink := sign.GenerateTokenFromString(link)
	h.App.InfoLog.Println("Signed link is", signedLink)

	// email the message
	var data struct {
		Link string
	}
	data.Link = signedLink

	msg := mailer.Message{
		To:       u.Email,
		Subject:  "Password reset",
		Template: "password-reset",
		Data:     data,
		From:     "admin@example.com",
	}

	h.App.Mail.Jobs <- msg
	res := <-h.App.Mail.Results
	if res.Error != nil {
		h.App.ErrorStatus(w, http.StatusBadRequest)
		return
	}

	// redirect the user
	http.Redirect(w, r, "/users/login", http.StatusSeeOther)
}

// ResetPasswordForm validates a signed url, and displays the password reset form, if appropriate
func (h *Handlers) ResetPasswordForm(w http.ResponseWriter, r *http.Request) {
	// get form values
	email := r.URL.Query().Get("email")
	theURL := r.RequestURI
	testURL := fmt.Sprintf("%s%s", h.App.Server.URL, theURL)

	// validate the url
	signer := urlsigner.Signer{
		Secret: []byte(h.App.EncryptionKey),
	}

	valid := signer.VerifyToken(testURL)
	if !valid {
		h.App.ErrorLog.Print("Invalid url")
		h.App.ErrorUnauthorized(w, r)
		return
	}

	/// make sure it's not expired
	expired := signer.Expired(testURL, 60)
	if expired {
		h.App.ErrorLog.Print("Link expired")
		h.App.ErrorUnauthorized(w, r)
		return
	}

	// display form
	encryptedEmail, _ := h.encrypt(email)

	vars := make(jet.VarMap)
	vars.Set("email", encryptedEmail)

	err := h.render(w, r, "reset-password", vars, nil)
	if err != nil {
		return
	}
}

func (h *Handlers) PostResetPassword(w http.ResponseWriter, r *http.Request) {
	// parse the form
	err := r.ParseForm()
	if err != nil {
		h.App.Error500(w, r)
		return
	}

	// get and decrypt the email
	email, err := h.decrypt(r.Form.Get("email"))
	if err != nil {
		h.App.Error500(w, r)
		return
	}

	// get the user
	var u data.User
	user, err := u.GetByEmail(email)
	if err != nil {
		h.App.Error500(w, r)
		return
	}

	// reset the password
	err = user.ResetPassword(user.ID, r.Form.Get("password"))
	if err != nil {
		h.App.Error500(w, r)
		return
	}

	// redirect
	h.App.Session.Put(r.Context(), "flash", "Password reset. You can now log in.")
	http.Redirect(w, r, "/users/login", http.StatusSeeOther)
}

func (h *Handlers) InitSocialAuth() {
	scope := []string{"user"}
	gScope := []string{"email", "profile"}

	goth.UseProviders(
		github.New(os.Getenv("GITHUB_KEY"), os.Getenv("GITHUB_SECRET"), os.Getenv("GITHUB_CALLBACK"), scope...),
		google.New(os.Getenv("GOOGLE_KEY"), os.Getenv("GOOGLE_SECRET"), os.Getenv("GOOGLE_CALLBACK"), gScope...),
	)

	key := os.Getenv("KEY")
	maxAge := 86400 * 30
	st := sessions.NewCookieStore([]byte(key))
	st.MaxAge(maxAge)
	st.Options.Path = "/"
	st.Options.HttpOnly = true
	st.Options.Secure = false

	gothic.Store = st
}

func (h *Handlers) SocialLogin(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	h.App.Session.Put(r.Context(), "social_provider", provider)
	h.InitSocialAuth()

	if _, err := gothic.CompleteUserAuth(w, r); err == nil {
		// user is already logged in
		http.Redirect(w, r, "/", http.StatusSeeOther)
	} else {
		// attempt social login
		gothic.BeginAuthHandler(w, r)
	}
}

func (h *Handlers) SocialMediaCallback(w http.ResponseWriter, r *http.Request) {
	h.InitSocialAuth()
	gUser, err := gothic.CompleteUserAuth(w, r)
	if err != nil {
		h.App.Session.Put(r.Context(), "error", err.Error())
		http.Redirect(w, r, "/users/login", http.StatusSeeOther)
		return
	}

	// look up user using email address
	var u data.User
	var testUser *data.User

	testUser, err = u.GetByEmail(gUser.Email)
	if err != nil {
		log.Println(err)
		provider := h.App.Session.Get(r.Context(), "social_provider").(string)
		// we don't have a user, so add one
		var newUser data.User
		if provider == "github" {
			exploded := strings.Split(gUser.Name, " ")
			newUser.FirstName = exploded[0]
			if len(exploded) > 1 {
				newUser.LastName = exploded[1]
			}
		} else {
			newUser.FirstName = gUser.FirstName
			newUser.LastName = gUser.LastName
		}
		newUser.Email = gUser.Email
		newUser.Active = 1
		newUser.Password = h.randomString(20)
		newUser.CreatedAt = time.Now()
		newUser.UpdatedAt = time.Now()
		_, err := newUser.Insert(newUser)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		testUser, _ = u.GetByEmail(gUser.Email)
	}

	h.App.Session.Put(r.Context(), "userID", testUser.ID)
	h.App.Session.Put(r.Context(), "social_token", gUser.AccessToken)
	h.App.Session.Put(r.Context(), "social_email", gUser.Email)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) socialLogout(w http.ResponseWriter, r *http.Request) {
	provider, ok := h.App.Session.Get(r.Context(), "social_provider").(string)
	if !ok {
		return
	}

	// call the appropriate api for our provider and revoke
	// the auth token. Each provider has different logic for doing this
	// (if it exists at all)

	switch provider {
	case "github":
		clientID := os.Getenv("GITHUB_KEY")
		clientSecret := os.Getenv("GITHUB_SECRET")

		token := h.App.Session.Get(r.Context(), "social_token").(string)

		var payload struct {
			AccessToken string `json:"access_token"`
		}
		payload.AccessToken = token

		jsonReq, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodDelete,
			fmt.Sprintf("https://%s:%s@api.github.com/applications/%s/grant", clientID, clientSecret, clientID),
			bytes.NewBuffer(jsonReq))
		if err != nil {
			h.App.ErrorLog.Println(err)
			return
		}

		client := &http.Client{}
		_, err = client.Do(req)
		if err != nil {
			h.App.ErrorLog.Println("Error logging out of Github:", err)
			return
		}

	case "google":
		token := h.App.Session.Get(r.Context(), "social_token").(string)
		_, err := http.PostForm(fmt.Sprintf("https://accounts.google.com/o/oauth2/revoke?%s", token), nil)
		if err != nil {
			h.App.ErrorLog.Println("Error logging out of Google:", err)
		}

	}
}