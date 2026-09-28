package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/marcopiovanello/yt-dlp-web-ui/v4/server/config"
	middlewares "github.com/marcopiovanello/yt-dlp-web-ui/v4/server/middleware"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || middlewares.OriginAllowed(r, origin, config.Instance().CORS.AllowedOrigins)
	},
}

// WebSockets JSON-RPC handler
func WebSocket(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer c.Close()

	// notify client that conn is open and ok
	c.WriteJSON(struct{ Status string }{Status: "connected"})

	for {
		mtype, reader, err := c.NextReader()
		if err != nil {
			break
		}

		request, err := io.ReadAll(reader)
		if err != nil {
			break
		}
		var res io.Reader
		method, parseErr := singleRPCMethod(request)
		if parseErr != nil {
			res = invalidRPCRequestResponse()
		} else if adminRPCMethod(method) && !isAdmin(r) {
			res = adminRequiredResponse(request)
		} else {
			res = newRequest(bytes.NewReader(request)).Call()
		}

		writer, err := c.NextWriter(mtype)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			break
		}

		io.Copy(writer, res)
	}
}

// HTTP-POST JSON-RPC handler
func Post(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	request, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	method, err := singleRPCMethod(request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var res io.Reader
	if adminRPCMethod(method) && !isAdmin(r) {
		res = adminRequiredResponse(request)
	} else {
		res = newRequest(bytes.NewReader(request)).Call()
	}
	_, err = io.Copy(w, res)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func singleRPCMethod(request []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(request))
	var call struct {
		Method string `json:"method"`
	}
	if err := decoder.Decode(&call); err != nil {
		return "", err
	}
	if call.Method == "" {
		return "", errors.New("JSON-RPC request is missing its method")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return "", errors.New("only one JSON-RPC request is allowed per message")
		}
		return "", err
	}
	return call.Method, nil
}

func adminRPCMethod(method string) bool {
	return (config.Instance().Authentication.RequireAuth || config.Instance().OpenId.UseOpenId) &&
		method == "Service.UpdateExecutable"
}

func invalidRPCRequestResponse() io.Reader {
	response, _ := json.Marshal(struct {
		Error  string          `json:"error"`
		Result json.RawMessage `json:"result"`
		ID     json.RawMessage `json:"id"`
	}{
		Error:  "only one valid JSON-RPC request is allowed per message",
		Result: json.RawMessage("null"),
		ID:     json.RawMessage("null"),
	})
	return bytes.NewReader(response)
}

func isAdmin(r *http.Request) bool {
	principal, ok := middlewares.PrincipalFromContext(r.Context())
	return ok && principal.IsAdmin
}

func adminRequiredResponse(request []byte) io.Reader {
	var call struct {
		ID json.RawMessage `json:"id"`
	}
	_ = json.Unmarshal(request, &call)
	if len(call.ID) == 0 {
		call.ID = json.RawMessage("null")
	}
	response, _ := json.Marshal(struct {
		Error  string          `json:"error"`
		Result json.RawMessage `json:"result"`
		ID     json.RawMessage `json:"id"`
	}{
		Error:  "administrator access required",
		Result: json.RawMessage("null"),
		ID:     call.ID,
	})
	return bytes.NewReader(response)
}
