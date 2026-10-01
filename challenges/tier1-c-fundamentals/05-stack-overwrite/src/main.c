/* packet: receives one network packet, checks its checksum, and hands it on. */
#include <ctype.h>
#include <stdio.h>
#include <string.h>
#include "flag_blob.h"

/* One received packet: its payload and the checksum computed on arrival. */
struct frame {
	char buf[16];
	unsigned checksum;
};

static void report(unsigned key)
{
	char out[30];
	flag_decode(key, out);
	for (int i = 0; out[i]; i++)
		if (!isprint((unsigned char)out[i]))
			out[i] = '?';
	printf("report: %s\n", out);
}

static unsigned sum(const unsigned char *p, size_t n)
{
	unsigned s = 0;
	for (size_t i = 0; i < n; i++)
		s = s * 31 + p[i];
	return s;
}

/* Copies the packet into a frame and checks it arrived intact. */
static int receive(const unsigned char *packet, size_t len)
{
	struct frame f;
	f.checksum = sum(packet, len);
	memcpy(f.buf, packet, len);
	if (f.checksum != sum(packet, len)) {
		printf("checksum mismatch: %08x, dropped\n", f.checksum);
		return -1;
	}
	printf("packet ok, checksum %08x\n", f.checksum);
	report(f.checksum);
	return 0;
}

int main(void)
{
	static const unsigned char packet[] = {
		0x47, 0x44, 0x42, 0x01, 0x00, 0x10, 0x7f, 0x00, 0x00, 0x01,
		0x1f, 0x90, 0x53, 0x59, 0x4e, 0x21, 0xde, 0xad, 0xbe, 0xef,
	};
	printf("received %zu bytes\n", sizeof packet);
	return receive(packet, sizeof packet) == 0 ? 0 : 1;
}
