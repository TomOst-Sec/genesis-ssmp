interface Shape {
    area(): number;
    perimeter(): number;
}

class Circle implements Shape {
    constructor(private radius: number) {}

    area(): number {
        return Math.PI * this.radius * this.radius;
    }

    perimeter(): number {
        return 2 * Math.PI * this.radius;
    }
}

class Rectangle implements Shape {
    constructor(private width: number, private height: number) {}

    area(): number {
        return this.width * this.height;
    }

    perimeter(): number {
        return 2 * (this.width + this.height);
    }
}

function createShape(type_name: string): Shape {
    if (type_name === "circle") {
        return new Circle(5);
    }
    return new Rectangle(4, 6);
}

const shape = createShape("circle");
const a = shape.area();
