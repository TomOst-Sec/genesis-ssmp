class Calculator:
    def __init__(self, value=0):
        self.value = value

    def add(self, n):
        self.value += n
        return self

    def subtract(self, n):
        self.value -= n
        return self


def factorial(n):
    if n <= 1:
        return 1
    return n * factorial(n - 1)


def fibonacci(n):
    if n <= 1:
        return n
    return fibonacci(n - 1) + fibonacci(n - 2)


def main():
    calc = Calculator(10)
    calc.add(5)
    result = factorial(5)
    fib = fibonacci(10)
    print(result, fib)
