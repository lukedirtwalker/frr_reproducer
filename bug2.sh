#!/bin/sh

# Add first route, should trigger BGP UPDATE adv
ip route add 10.1.0.0/16 dev eth0
sleep 1
# Del route1, the withdraw only BGP UPDATE is sent regardless of the MRAI timer.
ip route del 10.1.0.0/16 dev eth0
sleep 0.25
ip route add 10.1.0.0/16 dev eth0
