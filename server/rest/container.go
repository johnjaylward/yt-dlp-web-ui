package rest

import (
	"github.com/go-chi/chi/v5"
	middlewares "github.com/marcopiovanello/yt-dlp-web-ui/v4/server/middleware"
)

func Container(args *ContainerArgs) *Handler {
	var (
		service = ProvideService(args)
		handler = ProvideHandler(service)
	)
	return handler
}

func ApplyRouter(args *ContainerArgs) func(chi.Router) {
	h := Container(args)

	return func(r chi.Router) {
		r.Use(middlewares.ApplyAuthenticationByConfig)
		r.Post("/exec", h.Exec())
		r.Post("/execPlaylist", h.ExecPlaylist())
		r.Post("/execLivestream", h.ExecLivestream())
		r.Get("/running", h.Running())
		r.Get("/version", h.GetVersion())
		r.With(middlewares.AdminOnly).Get("/cookies", h.GetCookies())
		r.With(middlewares.AdminOnly).Post("/cookies", h.SetCookies())
		r.With(middlewares.AdminOnly).Delete("/cookies", h.DeleteCookies())
		r.Post("/template", h.AddTemplate())
		r.Patch("/template", h.UpdateTemplate())
		r.Get("/template/all", h.GetTemplates())
		r.Delete("/template/{id}", h.DeleteTemplate())
	}
}
