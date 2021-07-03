This is an example of a port scanner project for specified networks, written in golang. To run, you need a socks proxy and a json-formatted configuration file.  

Pass the path to config via c=config.json 

Example config for use:  
```json 
{
    "Networks" : [
        "192.168.1.0/30",
        "192.168.1.0/29"
    ],
    "Ports" : [
        "443,80,22",
        "1024-1027"
    ],
    "Socks" : [
        "127.0.0.1:1080"
    ],
    "ConcurrentlyHost" : 10,
    "ConcurrentlyPerHost" : 10,
    "ConnectionTimeout": 5,
    "ShowErrors": false 
}
```
