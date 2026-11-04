import os
import socket
import struct
import sys
import threading

if os.name == "nt":
    import msvcrt

    msvcrt.setmode(0, os.O_BINARY)
    msvcrt.setmode(1, os.O_BINARY)

sock = socket.create_connection(("127.0.0.1", 10808), timeout=10)


def read_exact(size):
    data = b""
    while len(data) < size:
        part = sock.recv(size - len(data))
        if not part:
            raise RuntimeError("SOCKS proxy closed during handshake")
        data += part
    return data


sock.sendall(b"\x05\x01\x00")
if read_exact(2) != b"\x05\x00":
    raise RuntimeError("SOCKS proxy requires unsupported authentication")

hostname = sys.argv[1].encode("ascii")
sock.sendall(
    b"\x05\x01\x00\x03" + bytes([len(hostname)]) + hostname
    + struct.pack("!H", int(sys.argv[2]))
)
reply = read_exact(4)
if reply[:2] != b"\x05\x00":
    raise RuntimeError("SOCKS proxy rejected the connection")
if reply[3] == 1:
    read_exact(6)
elif reply[3] == 4:
    read_exact(18)
elif reply[3] == 3:
    read_exact(read_exact(1)[0] + 2)
else:
    raise RuntimeError("SOCKS proxy returned an invalid address type")

sock.settimeout(None)


def stdin_to_socket():
    try:
        while True:
            data = os.read(0, 65536)
            if not data:
                sock.shutdown(socket.SHUT_WR)
                return
            sock.sendall(data)
    except OSError:
        pass


threading.Thread(target=stdin_to_socket, daemon=True).start()
try:
    while True:
        data = sock.recv(65536)
        if not data:
            break
        os.write(1, data)
finally:
    sock.close()
