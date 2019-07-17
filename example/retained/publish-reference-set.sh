#!/bin/bash -ex
if [ -z "$1" ];  then
    echo "Give the broker address as argument"
    exit 1
fi
broker=$1
mosquitto_pub -r -q 1 -h $broker -t '/mq-hammer/retained/a/1' -m 'rollulus1'
mosquitto_pub -r -q 1 -h $broker -t '/mq-hammer/retained/a/b/c/2' -m '2rollulus'
mosquitto_pub -r -q 1 -h $broker -t '/mq-hammer/retained/b/3' -m '3rollulus3'
echo "published reference set"
