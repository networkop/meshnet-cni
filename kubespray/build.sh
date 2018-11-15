#!/bin/bash

ansible-playbook -i hosts.ini --become --become-user=root -u core meshnet.yml
