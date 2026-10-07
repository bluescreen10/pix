// normal_matrix.glsl — how a transform turns a surface's normal.
#ifndef PIX_NORMAL_MATRIX_GLSL
#define PIX_NORMAL_MATRIX_GLSL

// normalMatrix turns a normal as transform m turns its surface: by m's inverse
// transpose, which keeps it perpendicular to the surface under any scale. m's own upper
// 3x3 does only while the scale is uniform: stretched along one axis, a slope lies
// flatter, but its normal turned by m stands steeper.
//
// It is the cofactor matrix, the inverse transpose times the determinant: three cross
// products, no inverse. The length it leaves the normal with is the caller's to remove,
// as the fragment stage does anyway; its sign is corrected here, so that a mirroring
// transform, whose determinant is negative, does not turn the normal inside out.
mat3 normalMatrix(mat4 m) {
    vec3 x = m[0].xyz;
    vec3 y = m[1].xyz;
    vec3 z = m[2].xyz;
    mat3 cofactors = mat3(cross(y, z), cross(z, x), cross(x, y));
    return dot(x, cross(y, z)) < 0.0 ? -cofactors : cofactors;
}

#endif // PIX_NORMAL_MATRIX_GLSL
