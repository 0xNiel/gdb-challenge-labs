/* TEMPLATE: the smallest program with the shape every lab has (challenges/README.md).
 * One bug, visible from a plain run; report() decodes the flag only from the right state. */
#include <ctype.h>
#include <stdio.h>
#include "flag_blob.h"

/* Prints the decoded flag; with the wrong key, 29 characters of garbage. Unprintable bytes
 * become '?' so a wrong key cannot upset the terminal. */
static void report(unsigned key)
{
	char out[30];
	flag_decode(key, out);
	for (int i = 0; out[i]; i++)
		if (!isprint((unsigned char)out[i]))
			out[i] = '?';
	printf("report: %s\n", out);
}

int main(void)
{
	unsigned answer = 41; /* the bug: the answer is 42 */
	printf("answer = %u\n", answer);
	report(answer);
	return 0;
}
