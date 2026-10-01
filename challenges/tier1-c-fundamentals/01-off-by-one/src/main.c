/* scores: averages five exam scores. */
#include <ctype.h>
#include <stdio.h>
#include "flag_blob.h"

#define N 5

/* The class's scores, and the curve applied later in the term. */
static struct gradebook {
	int scores[N];
	int curve;
} book = {{72, 85, 91, 64, 88}, 37};

static void report(unsigned key)
{
	char out[30];
	flag_decode(key, out);
	for (int i = 0; out[i]; i++)
		if (!isprint((unsigned char)out[i]))
			out[i] = '?';
	printf("report: %s\n", out);
}

/* Adds up n scores. */
static int sum_scores(const int *scores, int n)
{
	int total = 0;
	for (int i = 0; i <= n; i++)
		total += scores[i];
	return total;
}

int main(void)
{
	int total = sum_scores(book.scores, N);
	printf("total of %d scores: %d\n", N, total);
	printf("average: %.1f\n", total / (double)N);
	report((unsigned)total);
	return 0;
}
