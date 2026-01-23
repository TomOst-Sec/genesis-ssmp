"""Tests for calc module."""

from calc import add, multiply, MathHelper


def test_add():
    assert add(2, 3) == 5
    assert add(-1, 1) == 0
    assert add(0, 0) == 0


def test_multiply():
    assert multiply(2, 3) == 6
    assert multiply(-1, 5) == -5
    assert multiply(0, 100) == 0
