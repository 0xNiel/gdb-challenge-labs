/* checkout: prices a cart and applies a discount code if it is a valid one. */
#include <ctype.h>
#include <stdio.h>
#include <string.h>
#include "flag_blob.h"

static const char *codes[] = {"WELCOME5", "SUMMER-10", "LOYAL15"};

static void report(unsigned key)
{
	char out[30];
	flag_decode(key, out);
	for (int i = 0; out[i]; i++)
		if (!isprint((unsigned char)out[i]))
			out[i] = '?';
	printf("report: %s\n", out);
}

/* Sums the cart's line items, in cents. */
static unsigned cart_total(void)
{
	unsigned items[4] = {1299, 2450, 899, 351};
	unsigned total = 0;
	for (int i = 0; i < 4; i++)
		total += items[i];
	return total;
}

/* Returns 1 if code looks like a discount code: 4 to 12 capitals, digits or dashes. */
static int well_formed(const char *code)
{
	int ok = 1;
	for (size_t i = 0; code[i]; i++)
		if (!isupper((unsigned char)code[i]) && !isdigit((unsigned char)code[i]) && code[i] != '-') {
			ok = 0;
			break;
		}
	return ok && strlen(code) >= 4 && strlen(code) <= 12;
}

/* Returns 1 if code is one of the valid discount codes. */
static int lookup(const char *code)
{
	int found;
	for (size_t i = 0; i < sizeof codes / sizeof codes[0]; i++)
		if (strcmp(codes[i], code) == 0) {
			found = 1;
			break;
		}
	return found;
}

static unsigned apply_discount(unsigned price)
{
	return price - price / 10;
}

int main(void)
{
	const char *code = "SPRING10";
	unsigned price = cart_total();
	printf("cart: %u.%02u\n", price / 100, price % 100);
	if (!well_formed(code)) {
		printf("code %s is not a discount code\n", code);
	} else if (lookup(code)) {
		printf("code %s accepted: 10%% off\n", code);
		price = apply_discount(price);
	} else {
		printf("code %s is not valid\n", code);
	}
	printf("to pay: %u.%02u\n", price / 100, price % 100);
	report(price);
	return 0;
}
