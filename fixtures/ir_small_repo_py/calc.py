"""Simple calculator module with 3 symbols."""


def add(a: float, b: float) -> float:
    """Return the sum of a and b."""
    return a + b


def multiply(a: float, b: float) -> float:
    """Return the product of a and b."""
    return a * b


class MathHelper:
    """Helper class that uses add and multiply."""

    def sum_and_product(self, a: float, b: float) -> tuple:
        """Return (sum, product) of a and b."""
        return add(a, b), multiply(a, b)
