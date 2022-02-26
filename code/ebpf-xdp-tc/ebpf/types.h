struct observed_packet {
    __u32 sourceIp;
    __u32 destIp;
    __u16 sourcePort;
    __u16 destPort;
    enum {
        TCP,
        UDP
    } transport_proto;
};


struct two_tuple {
    __u32 ip;
    __u16 port;
    __u16 _pad;
};