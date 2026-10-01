/* badge: prints an access badge and its badge key for an employee. */
#include <ctype.h>
#include <stdio.h>
#include <string.h>
#include "flag_blob.h"

struct badge {
	char name[8];  /* up to 8 characters */
	char role[8];
	unsigned level;
};

static struct badge card;

static void report(unsigned key)
{
	char out[30];
	flag_decode(key, out);
	for (int i = 0; out[i]; i++)
		if (!isprint((unsigned char)out[i]))
			out[i] = '?';
	printf("report: %s\n", out);
}

/* The badge key: a hash of the name's first 8 bytes, mixed with its length. */
static unsigned badge_key(const struct badge *b)
{
	size_t len = strlen(b->name);
	unsigned h = 2166136261u;
	for (int i = 0; i < 8; i++) {
		h ^= (unsigned char)b->name[i];
		h *= 16777619u;
	}
	return h ^ (unsigned)len;
}

static void issue(struct badge *b, const char *name, const char *role, unsigned level)
{
	strncpy(b->name, name, sizeof b->name);
	strncpy(b->role, role, sizeof b->role);
	b->level = level;
}

int main(void)
{
	issue(&card, "Thompson", "admin", 4);
	printf("badge for %s (%zu characters), level %u\n", card.name, strlen(card.name), card.level);
	unsigned key = badge_key(&card);
	printf("badge key: %08x\n", key);
	report(key);
	return 0;
}
