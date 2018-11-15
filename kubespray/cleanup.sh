#!/bin/bash

ansible-playbook -i hosts.ini --become --become-user=root -u core -e clean=True meshnet.yml
