import { add, multiply, Calculator } from "./math";

function test_add() {
  console.assert(add(2, 3) === 5, "add(2,3) should be 5");
  console.assert(add(-1, 1) === 0, "add(-1,1) should be 0");
}

function test_calculator() {
  const calc = new Calculator();
  console.assert(calc.sum(2, 3) === 5, "sum(2,3) should be 5");
  console.assert(calc.product(2, 3) === 6, "product(2,3) should be 6");
}

test_add();
test_calculator();
console.log("All tests passed");
