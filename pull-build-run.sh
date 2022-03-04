#!/bin/bash

while :
do
	git pull && go build && sudo ./cheap-altari-bot /etc/letsencrypt/live/plusalpha.top/fullchain.pem /etc/letsencrypt/live/plusalpha.top/privkey.pem
done
