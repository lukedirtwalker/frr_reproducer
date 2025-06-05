#!/bin/sh

# Add first route, should trigger BGP UPDATE adv
ip route add 10.1.0.0/16 dev eth0
sleep 1
# Add second route, but no BGP UPDATE message yet, because MRAI timer.
ip route add 10.2.0.0/16 dev eth0
sleep 0.25
# Emulate route1 flap (del/add), where route1 will be eventually withdraw and the add
# is suppressed as a duplicate. This should happen within the MRAI timer.
ip route del 10.1.0.0/16 dev eth0
sleep 0.25
ip route add 10.1.0.0/16 dev eth0
