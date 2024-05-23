package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"websocket_ex/socket"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func main() {

	conn, _, _, err := ws.DefaultDialer.Dial(context.Background(), "ws://127.0.0.1:3000/websocket")

	if err != nil {
		panic(err)
	}

	packets := make(chan socket.Packet)

	go func() {
		for packet := range packets {
			js, _ := json.Marshal(packet)

			err := wsutil.WriteClientMessage(conn, ws.OpText, js)

			if err != nil {

				if strings.Contains(err.Error(), "broken pipe") {
					conn, _, _, err = ws.DefaultDialer.Dial(context.Background(), "ws://127.0.0.1:3000/websocket")

					if err != nil {
						fmt.Println(fmt.Errorf("[reconnecting] %v", err))
					}
				}
			}
		}
	}()

	r := chi.NewRouter()
	r.Use(middleware.Logger)

	r.Get("/index", func(w http.ResponseWriter, r *http.Request) {
		workDir, _ := os.Getwd()
		filesDir := filepath.Join(workDir, "static")
		if _, err := os.Stat(filesDir + r.URL.Path); errors.Is(err, os.ErrNotExist) {
			http.ServeFile(w, r, filepath.Join(filesDir, "index.html"))
		}
	})

	r.Post("/upload", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		r.ParseMultipartForm(50 << 20)

		fileResume, fileHeader, err := r.FormFile("myfile")

		if err != nil {
			panic("reading file error")
		}

		defer fileResume.Close()

		filepath, uploadErr := uploadSmallFiles(fileHeader.Filename, fileResume)

		if uploadErr != nil {
			w.Write([]byte("something was wrong with the upload!!"))
		} else {

			packets <- socket.Packet{
				Filepath: filepath,
			}

			w.Write([]byte("success"))
		}

	})

	r.Get("/download/{filepath}", func(w http.ResponseWriter, r *http.Request) {
		filepath := chi.URLParam(r, "filepath")
		fmt.Println(filepath)

		workDir, _ := os.Getwd()
		filepath = path.Join(workDir, "files", filepath)

		fmt.Println(filepath)

		if _, err := os.Stat(filepath); errors.Is(err, os.ErrNotExist) {
			w.WriteHeader(404)
			return
		}

		w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filepath))
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, filepath)
	})

	r.Get("/sse", func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		for {
			if msg, _, err := wsutil.ReadServerData(conn); err == nil {
				var res socket.Result

				err = json.NewDecoder(bytes.NewReader(msg)).Decode(&res)

				if err != nil {
					fmt.Println(err)
					continue
				}

				flusher, _ := w.(http.Flusher)

				w.Write([]byte(fmt.Sprintf("data: %s\n\n", filepath.Base(res.Converted))))

				flusher.Flush()

			}
		}
	})

	log.Fatal(http.ListenAndServe(":3500", r))
}

func uploadSmallFiles(filename string, file multipart.File) (filepath string, err error) {
	now := time.Now().UnixNano()
	parts := strings.Split(filename, ".")
	ext := parts[len(parts)-1]

	workDir, _ := os.Getwd()
	filepath = path.Join(workDir, "files", fmt.Sprintf("%v.%s", now, ext))
	out, err := os.Create(filepath)

	if err != nil {
		return filepath, err
	}

	defer out.Close()
	io.Copy(out, file)
	return filepath, nil
}
