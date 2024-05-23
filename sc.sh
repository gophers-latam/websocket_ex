#!/bin/bash

function req() {
	curl -F "myfile=@/home/user/Vídeos/rust_vs_go.mkv" http://localhost:3500/upload
}

export -f req
seq 1001 | parallel -j 5 --joblog log.log req

