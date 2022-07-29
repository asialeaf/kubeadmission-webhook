#!/bin/bash


CA_BUNDLE=$(cat /etc/webhook/admission/certs/ca.crt | base64)
echo $CA_BUNDLE | tr -d [" "]