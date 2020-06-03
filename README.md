# Shiny-Potato

This small tool can be used to deploy several alpine pods
with a persistent volume attached to it.

Regarding the name, I just took the first name GitHub proposed me :smile:

## Build

```
go build
```

## Usage

**deploy**


```
./shiny-potato deploy -storage-class csi-cephfs-sc -count 5
>>> Starting: 2020-06-03 21:33:54.002458925 +0200 CEST m=+0.018868854
>>> Creating Pod: default/shiny-potato-0001
>>> Creating PersistentVolumeClaim: default/shiny-potato-0001
>>> Creating Pod: default/shiny-potato-0002
>>> Creating PersistentVolumeClaim: default/shiny-potato-0002
>>> Creating Pod: default/shiny-potato-0003
>>> Creating PersistentVolumeClaim: default/shiny-potato-0003
>>> Creating Pod: default/shiny-potato-0004
>>> Creating PersistentVolumeClaim: default/shiny-potato-0004
>>> Creating Pod: default/shiny-potato-0005
>>> Creating PersistentVolumeClaim: default/shiny-potato-0005
>>> Finished: 2020-06-03 21:34:20.174090434 +0200 CEST m=+26.190500235
>>> Duration: 26.171676949s
```

**clean**

```
./shiny-potato clean -storage-class csi-cephfs-sc -count 5
>>> Starting: 2020-06-03 21:34:27.136615731 +0200 CEST m=+0.019372952
>>> Deleting Pod: default/shiny-potato-0001
>>> Deleting PersistentVolumeClaim: default/shiny-potato-0001
>>> Deleting Pod: default/shiny-potato-0002
>>> Deleting PersistentVolumeClaim: default/shiny-potato-0002
>>> Deleting Pod: default/shiny-potato-0003
>>> Deleting PersistentVolumeClaim: default/shiny-potato-0003
>>> Deleting Pod: default/shiny-potato-0004
>>> Deleting PersistentVolumeClaim: default/shiny-potato-0004
>>> Deleting PersistentVolumeClaim: default/shiny-potato-0005
>>> Deleting Pod: default/shiny-potato-0005
>>> Finished: 2020-06-03 21:35:13.297516429 +0200 CEST m=+46.180273661
>>> Duration: 46.160938436s
```
