package celeritas

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func (c *Celeritas) routes() http.Handler {
	mux := chi.NewRouter()
	mux.Use(middleware.RequestID)
	mux.Use(middleware.RealIP)
	if c.Debug {
		mux.Use(middleware.Logger)
	}
	mux.Use(middleware.Recoverer)
	mux.Use(c.SessionLoad)
	mux.Use(c.NoSurf)
	mux.Use(c.CheckForMaintenanceMode)

	return mux
}

// Routes are celeritas specific routes, which are mounted in the routes file
// in Celeritas applications
func Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/test-c", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("it works!"))
	})
	return r
}
