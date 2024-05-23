package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"reflect"
	"sync"
	"syscall"
	"websocket_ex/socket"

	"github.com/arl/statsviz"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	ffmpeg "github.com/u2takey/ffmpeg-go"
	"golang.org/x/sys/unix"
)

func main() {
	// Increase resources limitations
	var rLimit syscall.Rlimit

	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		panic(err)
	}

	rLimit.Cur = rLimit.Max

	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		panic(err)
	}

	// Start epoll
	var err error
	epoller, err = NewEpoll()
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	statsviz.Register(mux)

	go func() {
		log.Println(http.ListenAndServe("localhost:5151", mux))
	}()

	go Start()

	http.HandleFunc("/websocket", wsHandler)

	if err := http.ListenAndServe("0.0.0.0:3000", nil); err != nil {
		log.Fatal(err)
	}
}

func Start() {
	for {
		connections, err := epoller.Wait()

		if err != nil {
			log.Printf("Failed to epoll wait %v", err)
			continue
		}

		for _, conn := range connections {
			if conn == nil {
				break
			}

			if msg, op, err := wsutil.ReadClientData(conn); err != nil {

				if err := epoller.Remove(conn); err != nil {
					log.Printf("Failed to remove %v", err)
				}
				conn.Close()
			} else {

				var packet socket.Packet

				err = json.NewDecoder(bytes.NewReader(msg)).Decode(&packet)

				if err != nil {
					log.Println(err)
					continue
				}

				converted := packet.Filepath + ".mp4"
				err := ffmpeg.Input(packet.Filepath).
					Output(converted, ffmpeg.KwArgs{"c:v": "libx265"}).
					OverWriteOutput().ErrorToStdOut().Run()

				result := socket.Result{
					Packet:    packet,
					Err:       err,
					Converted: converted,
				}

				js, _ := json.Marshal(result)

				err = wsutil.WriteServerMessage(conn, op, js)

				if err != nil {
					log.Println(err)
				}

			}
		}
	}
}

var epoller *epoll

type epoll struct {
	fd          int
	connections map[int]net.Conn
	mu          *sync.RWMutex
}

func NewEpoll() (*epoll, error) {
	fd, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, err
	}
	return &epoll{
		fd:          fd,
		mu:          &sync.RWMutex{},
		connections: make(map[int]net.Conn),
	}, nil
}

func (e *epoll) Add(conn net.Conn) error {
	// Extract file descriptor associated with the connection
	fd := websocketFD(conn)
	err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_ADD, fd, &unix.EpollEvent{Events: unix.POLLIN | unix.POLLHUP, Fd: int32(fd)})

	if err != nil {
		return err
	}

	e.mu.Lock()

	defer e.mu.Unlock()

	e.connections[fd] = conn

	if len(e.connections)%100 == 0 {
		log.Printf("Total number of connections: %v", len(e.connections))
	}

	return nil
}

func (e *epoll) Remove(conn net.Conn) error {

	// Extract file descriptor associated with the connection
	fd := websocketFD(conn)

	// usa el controlador de epoll para enviar la syscalq ue elimina una conexión de epoll
	err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_DEL, fd, nil)

	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.connections, fd)

	if len(e.connections)%100 == 0 {
		log.Printf("Total number of connections: %v", len(e.connections))
	}

	return nil
}

func (e *epoll) Wait() ([]net.Conn, error) {
	events := make([]unix.EpollEvent, 100)
	n, err := unix.EpollWait(e.fd, events, 100)

	if err != nil {
		return nil, err
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	var connections []net.Conn

	for i := 0; i < n; i++ {
		conn := e.connections[int(events[i].Fd)]
		connections = append(connections, conn)
	}

	return connections, nil
}

func websocketFD(conn net.Conn) int {
	tcpConn := reflect.Indirect(reflect.ValueOf(conn)).FieldByName("conn")
	fdVal := tcpConn.FieldByName("fd")
	pfdVal := reflect.Indirect(fdVal).FieldByName("pfd")

	return int(pfdVal.FieldByName("Sysfd").Int())
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	// Upgrade connection
	conn, _, _, err := ws.UpgradeHTTP(r, w)

	if err != nil {
		return
	}

	if err := epoller.Add(conn); err != nil {
		log.Printf("Failed to add connection %v", err)
		conn.Close()
	}
}
