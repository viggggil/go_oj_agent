x=int(input()); l=0; r=x+1
while l<r:
    m=(l+r)//2
    if m*m<=x: l=m+1
    else: r=m
print(l-1)
