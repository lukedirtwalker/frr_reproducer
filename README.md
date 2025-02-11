# FRR bug reproducer

## Setup

To setup the containers run the following command:

```bash
docker-compose -f $PWD/docker-compose.yml up -d
```

## Topology

We have 2 frr containers (frr2 and frr3) connected to each other. One frr_test container is using the same network as frr2 it generates routes.

```text
+------------+                               +------------+
|    frr2    |-- 10.0.0.2 ------- 10.0.0.3 --|    frr3    |
+------------+                               +------------+
       |
+------------+
|  frr_test  |
+------------+
```

## Triggering the bug

Run the following command, it adds 2.5k /27 on frr2

```bash
docker exec frr2 bash -c "wget -q 10.42.0.2:80/add -O -; wget -q 10.42.0.2:80/del -O -; wget -q 10.42.0.2:80/add -O -"
```

Check the routes received on the frr3 container (check after at least 5 seconds (advertisement interval)):

```bash
docker exec frr3 vtysh -c  "show ip route summary"
```

If it shows 2503 the bug was not triggered, teardown and retry again. If it shows less the bug was triggered.

## Teardown

```bash
docker-compose -f $PWD/docker-compose.yml down
```
