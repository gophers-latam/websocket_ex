package socket

type Packet struct {
	Filepath string
}

type Result struct {
	Packet    Packet
	Err       error
	Converted string
}
