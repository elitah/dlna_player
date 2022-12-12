#!/bin/sh

basedir=`readlink -f ${0}`

if [ -z ${basedir} ]; then
  exit
fi

basedir=`dirname ${basedir}`

if [ -z ${basedir} ]; then
  exit
fi

cd ${basedir}

if [ -z `which go` ]; then
  echo -e "\033[31;1m#####################################################################################\033[0m"
  echo -e "\033[31;1mThis code must be compiling by golang. You can install it by typing:\033[0m"
  echo -e "\033[34;1msudo apt install golang-go\033[0m"
  echo -e "\033[31;1m#####################################################################################\033[0m"
  exit
fi

go mod tidy && go fmt && go build -ldflags "-s -w"
