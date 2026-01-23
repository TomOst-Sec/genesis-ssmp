/** Return the sum of a and b. */
export function add(a: number, b: number): number {
  return a + b;
}

/** Return the product of a and b. */
export function multiply(a: number, b: number): number {
  return a * b;
}

/** Calculator class that uses add and multiply. */
export class Calculator {
  sum(a: number, b: number): number {
    return add(a, b);
  }

  product(a: number, b: number): number {
    return multiply(a, b);
  }
}
