/* greeter: looks a user up by name and greets them. */
#include <ctype.h>
#include <stdio.h>
#include <string.h>
#include "flag_blob.h"

struct user {
	int id;
	char name[16];
	int level;
};

static struct user users[] = {
	{101, "alice", 3},
	{205, "carol", 1},
	{347, "bob", 2},
	{412, "dave", 5},
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

/* Returns the user called name, or NULL if there is none. */
static struct user *find_user(const char *name)
{
	for (size_t i = 0; i < sizeof users / sizeof users[0]; i++)
		if (strcmp(users[i].name, name) == 0)
			return &users[i];
	return NULL;
}

static void greet(struct user *u)
{
	printf("hello %s, you are level %d\n", u->name, u->level);
	report((unsigned)u->id);
}

int main(void)
{
	const char *who = "Bob";
	printf("looking up %s\n", who);
	struct user *u = find_user(who);
	greet(u);
	return 0;
}
