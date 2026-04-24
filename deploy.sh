#!/bin/bash
git pull
go build -o bin/server ./cmd/server
pm2 restart all 